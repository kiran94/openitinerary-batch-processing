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
	oibplogging "github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/notification"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry/metrics"
	"github.com/kiran94/openitinerary-batch-processing/pkg/logging"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	tracer   trace.Tracer
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
	tracer trace.Tracer,
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
		tracer:   tracer,
	}
}

func (w *BatchWorker) GetID() string {
	return w.id.String()
}

func (w *BatchWorker) Polling(ctx context.Context) (*queue.DequeueResult, error) {
	ctx, span := w.tracer.Start(ctx, string(telemetry.TracingSpanWorkerPolling))
	defer span.End()

	logger := w.logger.WithContext(ctx)
	logger.Info(ctx, "worker polling", "component", oibplogging.ComponentWorker)

	for {
		select {
		case <-ctx.Done():
			logger.Debug(ctx,
				"worker polling stopped",
				"component", oibplogging.ComponentWorker,
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
			logger.Debug(ctx,
				"queue was empty",
				"component", oibplogging.ComponentQueue,
			)
			continue
		}

		if err != nil {
			if ctx.Err() != nil {
				logger.Debug(ctx,
					"worker polling stopped",
					"component", oibplogging.ComponentWorker,
					"error", ctx.Err(),
				)
				return nil, ctx.Err()
			}

			if dequeueResult != nil {
				// The message returned from the queue is corrupted
				// We will attempt to delete it to stop it from reprocessing
				if dequeueResult.QueueReference != nil {
					if err := w.queue.DeleteMessage(ctx, *dequeueResult.QueueReference); err != nil {
						logger.Error(ctx,
							"invalid queue message deletion failed",
							"component", oibplogging.ComponentQueue,
							"error", err,
						)
						return nil, err
					}
				}
				continue
			}

			logger.Error(ctx,
				"queue dequeue failed",
				"component", oibplogging.ComponentQueue,
				"operation", oibplogging.OperationDequeue,
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
	component oibplogging.Component,
	operation oibplogging.Operation,
	logger logging.Logger,
	span trace.Span,
) error {
	logger.Error(ctx,
		"ticket operation failed",
		"component", component,
		"operation", operation,
		"status_reason", string(tsr),
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
		logger.Error(cleanupCtx,
			"ticket failure status update failed",
			"component", oibplogging.ComponentDatabase,
			"operation", oibplogging.OperationTicketUpdate,
			"status_reason", string(tsr),
			"error", updateErr,
			"original_error", err,
		)

		span.RecordError(updateErr)
		span.SetStatus(codes.Error, "")
	}

	err = errors.Join(err, updateErr)
	span.RecordError(err)
	span.SetStatus(codes.Error, "")
	return err
}

var ErrTicketAlreadyComplete = errors.New("ticket already complete")

func (w *BatchWorker) detectAlreadyCompleteJob(ctx context.Context, ticket *job.Ticket, dr *queue.DequeueResult, logger logging.Logger, span trace.Span) error {
	if ticket.Status != job.Complete {
		return nil
	}

	logger.Warn(ctx,
		"completed ticket was received again",
		"component", oibplogging.ComponentWorker,
		"operation", oibplogging.OperationDuplicateDelivery,
	)

	deleteMessageErr := w.queue.DeleteMessage(ctx, *dr.QueueReference)
	if deleteMessageErr != nil {
		logger.Error(ctx,
			"completed ticket message deletion failed",
			"component", oibplogging.ComponentQueue,
			"operation", oibplogging.OperationDeleteMessage,
			"error", deleteMessageErr,
		)

		span.SetStatus(codes.Error, "")
		span.RecordError(deleteMessageErr)
		return errors.Join(deleteMessageErr, ErrTicketAlreadyComplete)
	}

	return ErrTicketAlreadyComplete
}

func (w *BatchWorker) Start(ctx context.Context, dequeueResult queue.DequeueResult) error {
	ctx, span := w.tracer.Start(ctx, string(telemetry.TracingSpanProcessJob), trace.WithSpanKind(trace.SpanKindConsumer))
	defer span.End()

	logger := w.logger.WithContext(ctx)
	logger.Info(ctx,
		"worker started",
		"component", oibplogging.ComponentWorker,
	)

	span.SetAttributes(
		attribute.String("request_id", dequeueResult.Result.RequestID),
		attribute.String("ticket_id", dequeueResult.Result.TicketID),
		attribute.String("worker_id", w.id.String()),
	)

	metric := metrics.WorkerRunObservation{}
	defer func() {
		w.metrics.RecordWorkerRun(ctx, metric)
	}()

	ticketID := dequeueResult.Result.TicketID

	parsedID, err := uuid.Parse(ticketID)
	if err != nil {
		logger.Warn(ctx,
			"queue message contained an invalid ticket ID",
			"component", oibplogging.ComponentQueue,
			"operation", oibplogging.OperationDequeue,
			"error", err,
		)

		// If the Ticket ID pushed into the queue is not a valid then it suggests a corrupt message
		// We should log and delete the message from the queue to ensure that it does not repeatedly retry
		if deleteMessageErr := w.queue.DeleteMessage(ctx, *dequeueResult.QueueReference); deleteMessageErr != nil {
			logger.Error(ctx,
				"invalid ticket message deletion failed",
				"component", oibplogging.ComponentQueue,
				"operation", oibplogging.OperationDeleteMessage,
				"error", deleteMessageErr,
			)
			metric.Outcome = metrics.WorkerRunOutcomeFailed
			span.RecordError(deleteMessageErr)
			span.SetStatus(codes.Error, "")
			return errors.Join(err, deleteMessageErr)
		}

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		span.RecordError(err)
		span.SetStatus(codes.Error, "")
		return err
	}

	// Get the Ticket for that Job Database
	ticket, err := w.job.Get(ctx, parsedID)
	if err != nil {
		logger.Error(ctx,
			"ticket lookup failed",
			"component", oibplogging.ComponentDatabase,
			"operation", oibplogging.OperationTicketLookup,
			"error", err,
		)

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		span.RecordError(err)
		span.SetStatus(codes.Error, "")
		return err
	}

	// This handles a edge case where in a previous attempt, processing was marked as complete
	// but the DeleteMessage operation failed.
	// So we attempt to delete it again and stop processing here
	if err := w.detectAlreadyCompleteJob(ctx, ticket, &dequeueResult, logger, span); err != nil {
		if err == ErrTicketAlreadyComplete {
			span.AddEvent(ErrTicketAlreadyComplete.Error())
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
		logger.Error(ctx,
			"ticket processing status update failed",
			"component", oibplogging.ComponentDatabase,
			"operation", oibplogging.OperationTicketUpdate,
			"attempt_count", ticket.AttemptCount,
			"error", err,
		)

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		span.RecordError(err)
		span.SetStatus(codes.Error, "")
		return err
	}
	logger.Debug(ctx,
		"ticket marked for processing",
		"component", oibplogging.ComponentDatabase,
		"operation", oibplogging.OperationTicketUpdate,
		"attempt_count", ticket.AttemptCount,
	)

	span.SetAttributes(attribute.Int("attempt.count", int(ticket.AttemptCount)))

	reader, err := w.storage.Read(ctx, ticket.DocumentPath)
	if err != nil {
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		return w.updateTicketAndError(ctx, ticket, err, job.StorageReadFailed, oibplogging.ComponentStorage, oibplogging.OperationStorageRead, logger, span)
	}
	if reader == nil {
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		err := errors.New("storage returned a nil document reader")
		return w.updateTicketAndError(ctx, ticket, err, job.StorageReadNil, oibplogging.ComponentStorage, oibplogging.OperationStorageRead, logger, span)
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
	w.closeStream(ctx, reader, "source_document", logger)
	if convertErr != nil {
		w.closeStream(ctx, response, "adapter_response", logger)

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		err := w.classifyAdapterResponseSizeError(ctx, convertErr, logger)
		return w.updateTicketAndError(ctx, ticket, err, job.ConvertFailed, oibplogging.ComponentAdapter, oibplogging.OperationAdapterConvert, logger, span)
	}

	if response == nil {
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		err := errors.New("adapter returned a nil response reader")
		return w.updateTicketAndError(ctx, ticket, err, job.ResponseReaderNil, oibplogging.ComponentAdapter, oibplogging.OperationAdapterConvert, logger, span)
	}

	resultOutputPath := w.config.GetOutputPath(w.config.GetBucket(), w.config.GetBucketPath(), ticket.ID.String())
	logger.Debug(ctx,
		"writing processed ticket output",
		"component", oibplogging.ComponentStorage,
		"operation", oibplogging.OperationStorageWrite,
		"output_path", resultOutputPath,
	)

	storageUploadStartedAt := time.Now()
	countedResponse := &byteCountingReadCloser{ReadCloser: response}
	writeErr := w.storage.Write(ctx, countedResponse, resultOutputPath)
	w.closeStream(ctx, response, "adapter_response", logger)
	if writeErr != nil {
		w.metrics.RecordStorageUpload(ctx, metrics.StorageUploadObservation{
			Duration: time.Since(storageUploadStartedAt),
			Purpose:  metrics.StorageUploadPurposeResult,
			Outcome:  metrics.StorageUploadFailed,
		})

		metric.Outcome = metrics.WorkerRunOutcomeFailed
		err := w.classifyAdapterResponseSizeError(ctx, writeErr, logger)
		return w.updateTicketAndError(ctx, ticket, err, job.StorageWriteFailed, oibplogging.ComponentStorage, oibplogging.OperationStorageWrite, logger, span)
	}

	w.metrics.RecordStorageUpload(ctx, metrics.StorageUploadObservation{
		Duration: time.Since(storageUploadStartedAt),
		Purpose:  metrics.StorageUploadPurposeResult,
		Outcome:  metrics.StorageUploadSuccess,
	})
	w.metrics.RecordAdapterResponse(ctx, metrics.AdapterResponseObservation{
		SizeBytes:   countedResponse.BytesRead(),
		ContentType: config.ContentType(ticket.ContentType),
	})

	// Mark the Ticket Status
	ticket.UpdateStatus(job.Complete, nil, nil)
	if err := w.job.Update(ctx, ticket); err != nil {
		logger.Error(ctx,
			"ticket completion status update failed",
			"component", oibplogging.ComponentDatabase,
			"operation", oibplogging.OperationTicketUpdate,
			"error", err,
		)
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		span.SetStatus(codes.Error, "")
		span.RecordError(err)
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
		logger.Error(ctx,
			"processed ticket message deletion failed",
			"component", oibplogging.ComponentQueue,
			"operation", oibplogging.OperationDeleteMessage,
			"error", deleteMessageErr,
		)
		metric.Outcome = metrics.WorkerRunOutcomeFailed
		span.SetStatus(codes.Error, "")
		span.RecordError(deleteMessageErr)
		return deleteMessageErr
	}

	// Notify User
	notificationContent := fmt.Sprintf("Successfully processed ticket %v", ticket.ID)
	if err := w.notifier.Notify(ctx, "Processing Success", ticket.NotificationEmails, &notificationContent); err != nil {
		logger.Warn(ctx, "issue notifying", "component", oibplogging.ComponentNotifier, "operation", oibplogging.OperationNotificationSend, "error", err)
	}

	logger.Info(ctx,
		"ticket processed successfully",
		"component", oibplogging.ComponentWorker,
		"attempt_count", ticket.AttemptCount,
	)

	metric.Outcome = metrics.WorkerRunOutcomeSuccess
	return nil
}

func (w *BatchWorker) classifyAdapterResponseSizeError(ctx context.Context, err error, logger logging.Logger) error {
	var maxBytesErr *http.MaxBytesError
	if !errors.As(err, &maxBytesErr) {
		return err
	}

	logger.Error(ctx,
		"adapter response exceeded maximum size",
		"component", oibplogging.ComponentAdapter,
		"operation", oibplogging.OperationAdapterConvert,
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

func (w *BatchWorker) closeStream(ctx context.Context, stream io.Closer, streamName string, logger logging.Logger) {
	if stream == nil {
		logger.Debug(ctx,
			"nil stream was provided for closing",
			"component", oibplogging.ComponentWorker,
			"stream", streamName,
		)
		return
	}

	if err := stream.Close(); err != nil {
		logger.Warn(ctx,
			"worker stream close failed",
			"component", oibplogging.ComponentWorker,
			"stream", streamName,
			"error", err,
		)
	}
}
