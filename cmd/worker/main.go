package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/kiran94/openitinerary-batch-processing/internal"
	"github.com/kiran94/openitinerary-batch-processing/internal/clients"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/kiran94/openitinerary-batch-processing/internal/worker"
)

func main() {
	ctx := context.Background()

	config := config.ApplicationConfig{}
	logger := logging.New(config)

	storage, err := storage.NewAWSS3Storage(ctx, config)
	if err != nil {
		logger.Error("storage initialization failed", "component", logging.ComponentStorage, "backend", "s3", "error", err)
		os.Exit(1)
	}
	logger.Info("storage ready", "component", logging.ComponentStorage)

	db, err := internal.ConnectDatabase(logger)
	if err != nil {
		logger.Error("database initialization failed", "component", logging.ComponentDatabase, "error", err)
		os.Exit(1)
	}
	logger.Info("database ready", "component", logging.ComponentDatabase)

	queueService, err := queue.NewQueue(ctx, config, logger)
	if err != nil {
		logger.Error("queue initialization failed", "component", logging.ComponentQueue, "error", err)
		os.Exit(1)
	}
	logger.Info("queue ready", "component", logging.ComponentQueue)

	ticketRepository := job.NewTicketRepository(db)

	httpClient := &http.Client{Timeout: time.Duration(60) * time.Second}
	adapter := clients.NewAdapterClient(httpClient, logger, config.GetMaximumAdapterResponseSizeBytes())
	defer httpClient.CloseIdleConnections()

	numberWorkers := runtime.NumCPU()
	wg := &sync.WaitGroup{}

	workerErrors := make([]error, numberWorkers)

	for i := 0; i < numberWorkers; i++ {
		wg.Go(func() {
			processingItem, err := startWork(ctx, config, ticketRepository, queueService, storage, adapter, logger)

			if err != nil {

				if err != context.DeadlineExceeded {
					workerErrors[i] = err
				}

				// If a worker timeout happened and we were processing an item
				// then it may indicate the worker timeout is too low
				// If a worker timeout
				if err == context.DeadlineExceeded && processingItem {
					workerErrors[i] = err
				}

			}

		})
	}

	wg.Wait()

	err = errors.Join(workerErrors...)
	if err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}

func startWork(
	ctx context.Context,
	config config.Config,
	ticketRepository job.TicketRepository,
	queueService queue.BatchQueue,
	storage storage.Storage,
	adapter clients.Adapter,
	logger logging.Logger,
) (bool, error) {
	wr := worker.NewWorkReport()
	worker := worker.NewBatchWorker(config, ticketRepository, queueService, storage, adapter, logger)

	defer func() {
		logger.Info(
			"worker completed",
			"worker_id", worker.GetID(),
			"polling_duration_ms", wr.GetPollingDurationMs(),
			"processing_duration_ms", wr.GetProcessingTimeMs(),
			"overall_duration_ms", wr.GetOverallTimeMs(),
			"processed_item", wr.GetProcessedItem(),
			"error", wr.GetError(),
		)
	}()

	workerTimeout := time.Duration(config.GetWorkerTimeoutSeconds()) * time.Second
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(workerTimeout))
	defer cancel()

	dequeueResult, err := worker.Polling(ctx)
	wr.MarkPollingTime()
	if err != nil {
		logger.Debug("worker returned error during polling", "worker_id", worker.GetID(), "error", err)
		wr.SetError(err)
		return wr.GetProcessedItem(), err
	}
	wr.MarkProcessedItem()

	err = worker.Start(ctx, *dequeueResult)
	wr.MarkProcessingTime()
	if err != nil {
		logger.Debug("worker returned error during processing", "worker_id", worker.GetID(), "error", err)
		wr.SetError(err)
		return wr.GetProcessedItem(), err
	}

	return wr.GetProcessedItem(), nil
}
