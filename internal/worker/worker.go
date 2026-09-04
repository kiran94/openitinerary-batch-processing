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
	w.logger.Info(
		"worker polling",
		"worker_id", w.id.String(),
		"component", logging.ComponentWorker,
	)

	for {
		select {
		case <-ctx.Done():
			w.logger.Debug(
				"worker polling stopped",
				"component", logging.ComponentWorker,
				"worker_id", w.id.String(),
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
			w.logger.Debug(
				"queue was empty",
				"component", logging.ComponentQueue,
				"worker_id", w.id.String(),
			)
			continue
		}

		if err != nil {
			if ctx.Err() != nil {
				w.logger.Debug(
					"worker polling stopped",
					"component", logging.ComponentWorker,
					"worker_id", w.id.String(),
					"error", ctx.Err(),
				)
				return nil, ctx.Err()
			}

			if dequeueResult != nil {
				// The message returned from the queue is corrupted
				// We will attempt to delete it to stop it from reprocessing
				if dequeueResult.QueueReference != nil {
					if err := w.queue.DeleteMessage(ctx, *dequeueResult.QueueReference); err != nil {
						w.logger.Error(
							"invalid queue message deletion failed",
							"component", logging.ComponentQueue,
							"worker_id", w.id.String(),
							"error", err,
						)
						return nil, err
					}
				}
				continue
			}

			w.logger.Error(
				"queue dequeue failed",
				"component", logging.ComponentQueue,
				"worker_id", w.id.String(),
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
) error {
	w.logger.Error(
		"ticket processing failed",
		"component", logging.ComponentWorker,
		"worker_id", w.id.String(),
		"ticket_id", ticket.ID.String(),
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
		w.logger.Error(
			"ticket failure status update failed",
			"component", logging.ComponentDatabase,
			"worker_id", w.id.String(),
			"ticket_id", ticket.ID.String(),
			"status_reason", tsr,
			"error", updateErr,
			"original_error", err,
		)

	}

	return errors.Join(err, updateErr)
}

var ErrTicketAlreadyComplete = errors.New("ticket already complete")

func (w *BatchWorker) detectAlreadyCompleteJob(ctx context.Context, ticket *job.Ticket, dr *queue.DequeueResult) error {
	if ticket.Status != job.Complete {
		return nil
	}

	w.logger.Warn(
		"completed ticket was received again",
		"component", logging.ComponentWorker,
		"worker_id", w.id.String(),
		"ticket_id", ticket.ID.String(),
	)

	deleteMessageErr := w.queue.DeleteMessage(ctx, *dr.QueueReference)
	if deleteMessageErr != nil {
		w.logger.Error(
			"completed ticket message deletion failed",
			"component", logging.ComponentQueue,
			"worker_id", w.id.String(),
			"ticket_id", ticket.ID.String(),
			"error", deleteMessageErr,
		)
		return errors.Join(deleteMessageErr, ErrTicketAlreadyComplete)
	}

	return ErrTicketAlreadyComplete
}

func (w *BatchWorker) Start(ctx context.Context, dequeueResult queue.DequeueResult) error {
	w.logger.Info(
		"worker started",
		"worker_id", w.id.String(),
		"ticket_id", dequeueResult.Result.TicketID,
		"component", logging.ComponentWorker,
	)

	metric := metrics.WorkerRunObservation{}
	defer func() {
		w.metrics.RecordWorkerRun(ctx, metric)
	}()

	ticketID := dequeueResult.Result.TicketID

	parsedID, err := uuid.Parse(ticketID)
	if err != nil {
		w.logger.Warn(
			"queue message contained an invalid ticket ID",
			"component", logging.ComponentQueue,
			"worker_id", w.id.String(),
			"ticket_id", ticketID,
			"error", err,
		)

		// If the Ticket ID pushed into the queue is not a valid then it suggests a corrupt message
		// We should log and delete the message from the queue to ensure that it does not repeatedly retry
		if deleteMessageErr := w.queue.DeleteMessage(ctx, *dequeueResult.QueueReference); deleteMessageErr != nil {
			w.logger.Error(
				"invalid ticket message deletion failed",
				"component", logging.ComponentQueue,
				"worker_id", w.id.String(),
				"ticket_id", ticketID,
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
		w.logger.Error(
			"ticket lookup failed",
			"component", logging.ComponentDatabase,
			"worker_id", w.id.String(),
			"ticket_id", ticketID,
			"error", err,
		)

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		return err
	}

	// This handles a edge case where in a previous attempt, processing was marked as complete
	// but the DeleteMessage operation failed.
	// So we attempt to delete it again and stop processing here
	if err := w.detectAlreadyCompleteJob(ctx, ticket, &dequeueResult); err != nil {
		if err == ErrTicketAlreadyComplete {
			return nil
		} else {
			metric.Outcome = metrics.WorkerRunOutcomeFailed
			return err
		}
	}

	ticket.UpdateStatus(job.Processing, nil, nil)
	ticket.AttemptCount++
	if err := w.job.Update(ctx, ticket); err != nil {
		w.logger.Error(
			"ticket processing status update failed",
			"component", logging.ComponentDatabase,
			"worker_id", w.id.String(),
			"ticket_id", ticketID,
			"attempt_count", ticket.AttemptCount,
			"error", err,
		)

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		return err
	}
	w.logger.Debug(
		"ticket marked for processing",
		"component", logging.ComponentWorker,
		"worker_id", w.id.String(),
		"ticket_id", ticketID,
		"attempt_count", ticket.AttemptCount,
	)

	reader, err := w.storage.Read(ctx, ticket.DocumentPath)
	if err != nil {
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		return w.updateTicketAndError(ctx, ticket, err, job.StorageReadFailed)
	}
	if reader == nil {
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		err := errors.New("storage returned a nil document reader")
		return w.updateTicketAndError(ctx, ticket, err, job.StorageReadNil)
	}

	// Stream from Storage to the Adapter and stream the result from the adapter back to storage
	response, convertErr := w.adapter.Convert(ctx, reader, ticket.DocumentPath, ticket.ContentType)
	w.closeStream(reader, "source_document", w.id, ticket.ID)
	if convertErr != nil {
		w.closeStream(response, "adapter_response", w.id, ticket.ID)

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		err := w.classifyAdapterResponseSizeError(convertErr, ticket.ID)
		return w.updateTicketAndError(ctx, ticket, err, job.ConvertFailed)
	}

	if response == nil {
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		err := errors.New("adapter returned a nil response reader")
		return w.updateTicketAndError(ctx, ticket, err, job.ResponseReaderNil)
	}

	resultOutputPath := w.config.GetOutputPath(w.config.GetBucket(), w.config.GetBucketPath(), ticket.ID.String())
	w.logger.Debug(
		"writing processed ticket output",
		"component", logging.ComponentStorage,
		"worker_id", w.id.String(),
		"ticket_id", ticketID,
		"output_path", resultOutputPath,
	)

	storageUploadStartedAt := time.Now()
	writeErr := w.storage.Write(ctx, response, resultOutputPath)
	w.closeStream(response, "adapter_response", w.id, ticket.ID)
	if writeErr != nil {
		w.metrics.RecordStorageUpload(ctx, metrics.StorageUploadObservation{
			Duration: time.Since(storageUploadStartedAt),
			Purpose:  metrics.StorageUploadPurposeResult,
			Outcome:  metrics.StorageUploadFailed,
		})

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		err := w.classifyAdapterResponseSizeError(writeErr, ticket.ID)
		return w.updateTicketAndError(ctx, ticket, err, job.StorageWriteFailed)
	}

	w.metrics.RecordStorageUpload(ctx, metrics.StorageUploadObservation{
		Duration: time.Since(storageUploadStartedAt),
		Purpose:  metrics.StorageUploadPurposeResult,
		Outcome:  metrics.StorageUploadSuccess,
	})

	// Mark the Ticket Status
	ticket.UpdateStatus(job.Complete, nil, nil)
	if err := w.job.Update(ctx, ticket); err != nil {
		w.logger.Error(
			"ticket completion status update failed",
			"component", logging.ComponentDatabase,
			"worker_id", w.id.String(),
			"ticket_id", ticketID,
			"error", err,
		)
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		return err
	}

	deleteMessageErr := w.queue.DeleteMessage(ctx, *dequeueResult.QueueReference)
	if deleteMessageErr != nil {
		w.logger.Error(
			"processed ticket message deletion failed",
			"component", logging.ComponentQueue,
			"worker_id", w.id.String(),
			"ticket_id", ticketID,
			"error", deleteMessageErr,
		)
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		return deleteMessageErr
	}

	// Notify User
	notificationContent := fmt.Sprintf("Successfully processed ticket %v", ticket.ID)
	if err := w.notifier.Notify(ctx, "Processing Success", ticket.NotificationEmails, &notificationContent); err != nil {
		w.logger.Warn("issue notifying", "error", err, "ticket_id", ticketID)
	}

	w.logger.Info(
		"ticket processed successfully",
		"component", logging.ComponentWorker,
		"worker_id", w.id.String(),
		"ticket_id", ticketID,
		"attempt_count", ticket.AttemptCount,
	)
	metric.Outcome = metrics.WorkerRunOutcomeSuccess
	return nil
}

func (w *BatchWorker) classifyAdapterResponseSizeError(err error, ticketID uuid.UUID) error {
	var maxBytesErr *http.MaxBytesError
	if !errors.As(err, &maxBytesErr) {
		return err
	}

	w.logger.Error(
		"adapter response exceeded maximum size",
		"component", logging.ComponentWorker,
		"worker_id", w.id.String(),
		"ticket_id", ticketID.String(),
		"limit_bytes", maxBytesErr.Limit,
		"error", err,
	)
	return fmt.Errorf("adapter response exceeded maximum size: %w", err)
}

func (w *BatchWorker) closeStream(stream io.Closer, streamName string, workerID, ticketID uuid.UUID) {
	if stream == nil {
		w.logger.Debug(
			"nil stream was provided for closing",
			"component", logging.ComponentWorker,
			"stream", streamName,
			"worker_id", workerID.String(),
			"ticket_id", ticketID.String(),
		)
		return
	}

	if err := stream.Close(); err != nil {
		w.logger.Warn(
			"worker stream close failed",
			"component", logging.ComponentWorker,
			"stream", streamName,
			"worker_id", workerID.String(),
			"ticket_id", ticketID.String(),
			"error", err,
		)
	}
}
