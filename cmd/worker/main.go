package main

import (
	"context"
	"net/http"
	"os"
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

	// TODO: Split workers into multiple go routiunes

	worker := worker.NewBatchWorker(config, ticketRepository, queueService, storage, adapter, logger)

	processedItem := false
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		logger.Info("worker completed", "worker_id", worker.GetID(), "duration_ms", duration.Milliseconds(), "processed_item", processedItem)
	}()

	workerTimeout := time.Duration(config.GetWorkerTimeoutSeconds()) * time.Second
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(workerTimeout))
	defer cancel()

	dequeueResult, err := worker.Polling(ctx)
	if err != nil {
		panic(err)
	}

	processedItem = true
	if err := worker.Start(ctx, *dequeueResult); err != nil {
		panic(err)
	}
}
