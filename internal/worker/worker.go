package worker

import (
	"context"

	"github.com/kiran94/openitinerary-batch-processing/internal/clients"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
)

type Worker interface {
	Start(ctx context.Context) error
}

type BatchWorker struct {
	config  config.Config
	job     job.JobCreator
	queue   queue.BatchQueue
	storage storage.Storage
	adapter clients.Adapter
}

func (w *BatchWorker) Start(ctx context.Context) error {

	// Poll for message from the Queue

	// Get the Ticket for that Job Database

	// Stream from Strorage to the Adapter and stream the result from the adapter back to storage

	// Mark the Ticket Status

	// Notify User

	return nil
}
