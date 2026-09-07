package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/clients"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/notification"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry/metrics"
)

type Worker interface {
	GetID() string
	Polling(ctx context.Context) (*queue.DequeueResult, error)
	Start(ctx context.Context, dequeueResult queue.DequeueResult) error
}

type BatchWorker struct {
	id       uuid.UUID
	config   config.Config
	job      job.TicketRepository
	queue    queue.BatchQueue
	storage  storage.Storage
	adapter  clients.Adapter
	logger   logging.Logger
	notifier notification.Notifier
	metrics  telemetry.WorkerMetrics
}

func NewBatchWorker(
	config config.Config,
	job job.TicketRepository,
	queue queue.BatchQueue,
	storage storage.Storage,
	adapter clients.Adapter,
	logger logging.Logger,
	notifier notification.Notifier,
	metrics telemetry.WorkerMetrics,

) Worker {
	return &BatchWorker{
		id:       uuid.New(),
		config:   config,
		job:      job,
		queue:    queue,
		storage:  storage,
		adapter:  adapter,
		logger:   logger,
		notifier: notifier,
		metrics:  metrics,
	}
}

func (w *BatchWorker) GetID() string {
	return w.id.String()
}

func (w *BatchWorker) Polling(ctx context.Context) (*queue.DequeueResult, error) {
	logger := w.logger.WithContext(ctx)

	logger.Info(
		"worker polling",
		"component", logging.ComponentWorker,
	)

	for {
		select {
		case <-ctx.Done():
			logger.Debug(
				"worker polling stopped",
				"component", logging.ComponentWorker,
				"error", ctx.Err(),
			)
			return nil, ctx.Err()
		default:
			// pass down to next code
			// since there was no cancel
		}

		pollingStartedAt := time.Now()
		dequeueResult, err := w.queue.Dequeue(ctx)
		pollingOutcome := metrics.WorkerPollingOutcomeSuccess
		if err != nil {
			if ctx.Err() != nil {
				pollingOutcome = metrics.WorkerPollingOutcomeCancelled
			} else {
				pollingOutcome = metrics.WorkerPollingOutcomeFailed
			}
		}
		w.metrics.RecordWorkerPolling(ctx, metrics.WorkerPollingObservation{
			Duration: time.Since(pollingStartedAt),
			Outcome:  pollingOutcome,
		})

		if dequeueResult == nil && err == nil {
			logger.Debug(
				"queue was empty",
				"component", logging.ComponentQueue,
			)
			continue
		}

		if err != nil {
			if ctx.Err() != nil {
				logger.Debug(
					"worker polling stopped",
					"component", logging.ComponentWorker,
					"error", ctx.Err(),
				)
				return nil, ctx.Err()
			}

			if dequeueResult != nil {
				// The message returned from the queue is corrupted
				// We will attempt to delete it to stop it from reprocessing
				if dequeueResult.QueueReference != nil {
					if err := w.queue.DeleteMessage(ctx, *dequeueResult.QueueReference); err != nil {
						logger.Error(
							"invalid queue message deletion failed",
							"component", logging.ComponentQueue,
							"error", err,
						)
						return nil, err
					}
				}
				continue
			}

			logger.Error(
				"queue dequeue failed",
				"component", logging.ComponentQueue,
				"error", err,
			)

			return nil, err
		}

		if dequeueResult == nil {
			continue
		}

		return dequeueResult, nil
	}
}

func (w *BatchWorker) updateTicketAndError(
	ctx context.Context,
	ticket *job.Ticket,
	err error,
	tsr job.TicketStatusReason,
	logger logging.Logger,
) error {
	logger.Error(
		"ticket processing failed",
		"component", logging.ComponentWorker,
		"status_reason", tsr,
		"error", err,
	)

	// Wipe out the cancellation from the parent context and apply a small one now
	// In the case that the parent worker context timed out, we don't want the error
	// reporting datbase update to see that cancellation and immeditately fail.
	// There we create a fresh one.
	// We are adding a small timeout here because, in the case that there is some issue
	// communicating with the db then we don't want this thread to indefinitely stay running
	// The log above also acts as backup to ensure we capture the error somewhere.
	cleanupCtx := context.WithoutCancel(ctx)
	cleanupCtx, cancel := context.WithDeadline(cleanupCtx, time.Now().Add(time.Second*60))
	defer cancel()

	ticket.UpdateStatus(job.Failed, &tsr, err)
	updateErr := w.job.Update(cleanupCtx, ticket)
	if updateErr != nil {
		logger.Error(
			"ticket failure status update failed",
			"component", logging.ComponentDatabase,
			"status_reason", tsr,
			"error", updateErr,
			"original_error", err,
		)

	}

	return errors.Join(err, updateErr)
}

var ErrTicketAlreadyComplete = errors.New("ticket already complete")

func (w *BatchWorker) detectAlreadyCompleteJob(ctx context.Context, ticket *job.Ticket, dr *queue.DequeueResult, logger logging.Logger) error {
	if ticket.Status != job.Complete {
		return nil
	}

	logger.Warn(
		"completed ticket was received again",
		"component", logging.ComponentWorker,
	)

	deleteMessageErr := w.queue.DeleteMessage(ctx, *dr.QueueReference)
	if deleteMessageErr != nil {
		logger.Error(
			"completed ticket message deletion failed",
			"component", logging.ComponentQueue,
			"error", deleteMessageErr,
		)
		return errors.Join(deleteMessageErr, ErrTicketAlreadyComplete)
	}

	return ErrTicketAlreadyComplete
}

func (w *BatchWorker) Start(ctx context.Context, dequeueResult queue.DequeueResult) error {
	logger := w.logger.WithContext(ctx)

	logger.Info(
		"worker started",
		"component", logging.ComponentWorker,
	)

	metric := metrics.WorkerRunObservation{}
	defer func() {
		w.metrics.RecordWorkerRun(ctx, metric)
	}()

	ticketID := dequeueResult.Result.TicketID

	parsedID, err := uuid.Parse(ticketID)
	if err != nil {
		logger.Warn(
			"queue message contained an invalid ticket ID",
			"component", logging.ComponentQueue,
			"error", err,
		)

		// If the Ticket ID pushed into the queue is not a valid then it suggests a corrupt message
		// We should log and delete the message from the queue to ensure that it does not repeatedly retry
		if deleteMessageErr := w.queue.DeleteMessage(ctx, *dequeueResult.QueueReference); deleteMessageErr != nil {
			logger.Error(
				"invalid ticket message deletion failed",
				"component", logging.ComponentQueue,
				"error", deleteMessageErr,
			)
			metric.Outcome = metrics.WorkerRunOutcomeFailed
			return errors.Join(err, deleteMessageErr)
		}

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		return err
	}

	// Get the Ticket for that Job Database
	ticket, err := w.job.Get(ctx, parsedID)
	if err != nil {
		logger.Error(
			"ticket lookup failed",
			"component", logging.ComponentDatabase,
			"error", err,
		)

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		return err
	}

	// This handles a edge case where in a previous attempt, processing was marked as complete
	// but the DeleteMessage operation failed.
	// So we attempt to delete it again and stop processing here
	if err := w.detectAlreadyCompleteJob(ctx, ticket, &dequeueResult, logger); err != nil {
		if err == ErrTicketAlreadyComplete {
			return nil
		} else {
			metric.Outcome = metrics.WorkerRunOutcomeFailed
			return err
		}
	}

	// Start the processing observation after the ticket has been validated and
	// duplicate completed deliveries have been excluded. Keep the end time
	// separate so queue acknowledgement and notification are not measured.
	processingStartedAt := time.Now()
	processingOutcome := metrics.WorkerProcessingOutcomeFailed
	processingEndedAt := time.Time{}
	defer func() {
		end := processingEndedAt
		if end.IsZero() {
			end = time.Now()
		}
		w.metrics.RecordWorkerProcessing(ctx, metrics.WorkerProcessingObservation{
			Duration: end.Sub(processingStartedAt),
			Outcome:  processingOutcome,
		})
	}()

	ticket.UpdateStatus(job.Processing, nil, nil)
	ticket.AttemptCount++
	if err := w.job.Update(ctx, ticket); err != nil {
		logger.Error(
			"ticket processing status update failed",
			"component", logging.ComponentDatabase,
			"attempt_count", ticket.AttemptCount,
			"error", err,
		)

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		return err
	}
	logger.Debug(
		"ticket marked for processing",
		"component", logging.ComponentWorker,
		"attempt_count", ticket.AttemptCount,
	)

	reader, err := w.storage.Read(ctx, ticket.DocumentPath)
	if err != nil {
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		return w.updateTicketAndError(ctx, ticket, err, job.StorageReadFailed, logger)
	}
	if reader == nil {
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		err := errors.New("storage returned a nil document reader")
		return w.updateTicketAndError(ctx, ticket, err, job.StorageReadNil, logger)
	}

	// Stream from Storage to the Adapter and stream the result from the adapter back to storage
	conversionStartedAt := time.Now()
	response, convertErr := w.adapter.Convert(ctx, reader, ticket.DocumentPath, ticket.ContentType)
	conversionOutcome := metrics.AdapterConversionOutcomeSuccess
	if convertErr != nil || response == nil {
		conversionOutcome = metrics.AdapterConversionOutcomeFailed
	}
	w.metrics.RecordAdapterConversion(ctx, metrics.AdapterConversionObservation{
		Duration: time.Since(conversionStartedAt),
		Outcome:  conversionOutcome,
	})
	w.closeStream(reader, "source_document", logger)
	if convertErr != nil {
		w.closeStream(response, "adapter_response", logger)

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		err := w.classifyAdapterResponseSizeError(convertErr, logger)
		return w.updateTicketAndError(ctx, ticket, err, job.ConvertFailed, logger)
	}

	if response == nil {
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		err := errors.New("adapter returned a nil response reader")
		return w.updateTicketAndError(ctx, ticket, err, job.ResponseReaderNil, logger)
	}

	resultOutputPath := w.config.GetOutputPath(w.config.GetBucket(), w.config.GetBucketPath(), ticket.ID.String())
	logger.Debug(
		"writing processed ticket output",
		"component", logging.ComponentStorage,
		"output_path", resultOutputPath,
	)

	storageUploadStartedAt := time.Now()
	countedResponse := &byteCountingReadCloser{ReadCloser: response}
	writeErr := w.storage.Write(ctx, countedResponse, resultOutputPath)
	w.closeStream(response, "adapter_response", logger)
	if writeErr != nil {
		w.metrics.RecordStorageUpload(ctx, metrics.StorageUploadObservation{
			Duration: time.Since(storageUploadStartedAt),
			Purpose:  metrics.StorageUploadPurposeResult,
			Outcome:  metrics.StorageUploadFailed,
		})

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		err := w.classifyAdapterResponseSizeError(writeErr, logger)
		return w.updateTicketAndError(ctx, ticket, err, job.StorageWriteFailed, logger)
	}

	w.metrics.RecordStorageUpload(ctx, metrics.StorageUploadObservation{
		Duration: time.Since(storageUploadStartedAt),
		Purpose:  metrics.StorageUploadPurposeResult,
		Outcome:  metrics.StorageUploadSuccess,
	})
	w.metrics.RecordAdapterResponse(ctx, metrics.AdapterResponseObservation{
		SizeBytes:   countedResponse.BytesRead(),
		ContentType: metrics.ContentType(ticket.ContentType),
	})

	// Mark the Ticket Status
	ticket.UpdateStatus(job.Complete, nil, nil)
	if err := w.job.Update(ctx, ticket); err != nil {
		logger.Error(
			"ticket completion status update failed",
			"component", logging.ComponentDatabase,
			"error", err,
		)
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		return err
	}
	processingOutcome = metrics.WorkerProcessingOutcomeSuccess
	processingEndedAt = time.Now()

	w.metrics.RecordJobTerminal(ctx, metrics.TerminalObservation{Outcome: metrics.TerminalSuccess})
	w.metrics.RecordJobEndToEnd(ctx, metrics.JobEndObservation{
		Duration: time.Since(ticket.CreatedAt),
		Outcome:  metrics.JobEndOutcomeSuccess,
	})

	deleteMessageErr := w.queue.DeleteMessage(ctx, *dequeueResult.QueueReference)
	if deleteMessageErr != nil {
		logger.Error(
			"processed ticket message deletion failed",
			"component", logging.ComponentQueue,
			"error", deleteMessageErr,
		)
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		return deleteMessageErr
	}

	// Notify User
	notificationContent := fmt.Sprintf("Successfully processed ticket %v", ticket.ID)
	if err := w.notifier.Notify(ctx, "Processing Success", ticket.NotificationEmails, &notificationContent); err != nil {
		logger.Warn("issue notifying", "error", err)
	}

	logger.Info(
		"ticket processed successfully",
		"component", logging.ComponentWorker,
		"attempt_count", ticket.AttemptCount,
	)

	metric.Outcome = metrics.WorkerRunOutcomeSuccess
	return nil
}

func (w *BatchWorker) classifyAdapterResponseSizeError(err error, logger logging.Logger) error {
	var maxBytesErr *http.MaxBytesError
	if !errors.As(err, &maxBytesErr) {
		return err
	}

	logger.Error(
		"adapter response exceeded maximum size",
		"component", logging.ComponentWorker,
		"limit_bytes", maxBytesErr.Limit,
		"error", err,
	)
	return fmt.Errorf("adapter response exceeded maximum size: %w", err)
}

// byteCountingReadCloser counts bytes as the storage implementation consumes
// the adapter response. Counting at this boundary measures the bytes actually
// written to result storage without buffering the response in memory.
type byteCountingReadCloser struct {
	io.ReadCloser
	bytesRead int64
}

func (r *byteCountingReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.bytesRead += int64(n)
	return n, err
}

func (r *byteCountingReadCloser) BytesRead() int64 {
	return r.bytesRead
}

func (w *BatchWorker) closeStream(stream io.Closer, streamName string, logger logging.Logger) {
	if stream == nil {
		logger.Debug(
			"nil stream was provided for closing",
			"component", logging.ComponentWorker,
			"stream", streamName,
		)
		return
	}

	if err := stream.Close(); err != nil {
		logger.Warn(
			"worker stream close failed",
			"component", logging.ComponentWorker,
			"stream", streamName,
			"error", err,
		)
	}
}
