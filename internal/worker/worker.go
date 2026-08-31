package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/clients"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
)

type Worker interface {
	GetID() string
	Polling(ctx context.Context) (*queue.DequeueResult, error)
	Start(ctx context.Context, dequeueResult queue.DequeueResult) error
}

type BatchWorker struct {
	id      uuid.UUID
	config  config.Config
	job     job.TicketRepository
	queue   queue.BatchQueue
	storage storage.Storage
	adapter clients.Adapter
	logger  logging.Logger
}

func NewBatchWorker(
	config config.Config,
	job job.TicketRepository,
	queue queue.BatchQueue,
	storage storage.Storage,
	adapter clients.Adapter,
	logger logging.Logger,
) Worker {
	return &BatchWorker{
		id:      uuid.New(),
		config:  config,
		job:     job,
		queue:   queue,
		storage: storage,
		adapter: adapter,
		logger:  logger,
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
			w.logger.Warn("polling cancelled due to worker timeout", "worker_id", w.id.String())
			return nil, ctx.Err()
		default:
			// pass down to next code
			// since there was no cancel
		}

		dequeueResult, err := w.queue.Dequeue(ctx)
		if dequeueResult == nil && err == nil {
			w.logger.Debug("queue was empty", "worker_id", w.id.String())
			continue
		}

		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}

			if dequeueResult != nil {
				// The message returned from the queue is corrupted
				// We will attempt to delete it to stop it from reprocessing
				if dequeueResult.QueueReference != nil {
					if err := w.queue.DeleteMessage(ctx, *dequeueResult.QueueReference); err != nil {
						w.logger.Error(
							"job could not be deleted from the queue",
							"component", logging.ComponentWorker,
							"error", err,
						)
					}
				}
				continue
			}

			return nil, err
		}

		if dequeueResult == nil {
			continue
		}

		return dequeueResult, nil
	}
}

func (w *BatchWorker) updateTicketAndError(ticket *job.Ticket, err error, tsr job.TicketStatusReason) error {
	ticket.Error = new(err.Error())
	ticket.Status = job.Failed
	ticket.StatusReason = new(tsr)

	if updateErr := w.job.Update(ticket); updateErr != nil {
		w.logger.Error(
			"failed when attempting to mark job with error",
			"component", logging.ComponentWorker,
			"worker_id", w.id,
			"error", updateErr,
			"original_error", err,
		)

		return errors.Join(err, updateErr)
	}

	return err
}

func (w *BatchWorker) Start(ctx context.Context, dequeueResult queue.DequeueResult) error {
	w.logger.Info(
		"worker started",
		"worker_id", w.id.String(),
		"ticket_id", dequeueResult.Result.TicketID,
		"component", logging.ComponentWorker,
	)

	ticketID := dequeueResult.Result.TicketID

	parsedID, err := uuid.Parse(ticketID)
	if err != nil {
		return err
	}

	// Get the Ticket for that Job Database
	ticket, err := w.job.Get(parsedID)
	if err != nil {
		return err
	}

	ticket.Status = job.Processing
	ticket.AttemptCount++
	if err := w.job.Update(ticket); err != nil {
		return err
	}

	// TODO: Write better logs
	reader, err := w.storage.Read(ctx, ticket.DocumentPath)
	if err != nil {
		return w.updateTicketAndError(ticket, err, job.StorageReadFailed)
	}
	if reader == nil {
		err := errors.New("storage returned a nil document reader")
		return w.updateTicketAndError(ticket, err, job.StorageReadNil)
	}

	// Stream from Storage to the Adapter and stream the result from the adapter back to storage
	response, convertErr := w.adapter.Convert(ctx, reader, ticket.DocumentPath, ticket.ContentType)
	w.closeStream(reader, "source_document", w.id, ticket.ID)
	if convertErr != nil {
		w.closeStream(response, "adapter_response", w.id, ticket.ID)

		err := w.classifyAdapterResponseSizeError(convertErr, ticket.ID)
		return w.updateTicketAndError(ticket, err, job.ConvertFailed)
	}

	if response == nil {
		err := errors.New("adapter returned a nil response reader")
		return w.updateTicketAndError(ticket, err, job.ResponseReaderNil)
	}

	resultOutputPath := w.config.GetOutputPath(w.config.GetBucket(), w.config.GetBucketPath(), ticket.ID.String())
	writeErr := w.storage.Write(ctx, response, resultOutputPath)
	w.closeStream(response, "adapter_response", w.id, ticket.ID)
	if writeErr != nil {
		err := w.classifyAdapterResponseSizeError(writeErr, ticket.ID)
		return w.updateTicketAndError(ticket, err, job.StorageWriteFailed)
	}

	// Mark the Ticket Status
	ticket.Status = job.Complete
	ticket.Error = nil
	ticket.StatusReason = nil
	if err := w.job.Update(ticket); err != nil {
		return err
	}

	// Notify User
	// TODO

	deleteMessageErr := w.queue.DeleteMessage(ctx, *dequeueResult.QueueReference)
	if deleteMessageErr != nil {
		w.logger.Error("delete message failed", "component", logging.ComponentWorker, "error", deleteMessageErr)
		return deleteMessageErr
	}

	w.logger.Info("worker processed successfully", "worker_id", w.id, "ticket_id", ticketID)
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
		w.logger.Debug("nil stream was provided for closing", "stream", streamName)
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
