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
	"github.com/kiran94/openitinerary-batch-processing/internal/notification"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"github.com/kiran94/openitinerary-batch-processing/internal/worker"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	config := config.ApplicationConfig{}
	logger := logging.New(config)

	otelMetricProvider, err := telemetry.NewMeterManager(ctx, config.GetOtelWorkerName(), config)
	if err != nil {
		logger.Error("storage initialization failed", "component", logging.ComponentStorage, "backend", "s3", "error", err)
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithDeadline(ctx, time.Now().Add(60*time.Second))
		defer cancel()

		if err := otelMetricProvider.Close(cleanupCtx); err != nil {
			logger.Error("failed to cleanup otel", "error", err)
		}
	}()
	if err := otelMetricProvider.Ready(); err != nil {
		logger.Error("otel ready failed", "error", err)
		return err
	}
	telemetry.SetLogger(logger)

	serviceMetrics, err := telemetry.NewApplicationOtelMetrics(otelMetricProvider.GetProvider())
	if err != nil {
		logger.Error("metrics initialization failed", "error", err)
		return err
	}
	logger.Info("telemtry ready", "component", logging.ComponentTelemetry)

	storage, err := storage.NewAWSS3Storage(ctx, config)
	if err != nil {
		logger.Error("storage initialization failed", "component", logging.ComponentStorage, "backend", "s3", "error", err)
		return err
	}
	logger.Info("storage ready", "component", logging.ComponentStorage, "backend", "s3")

	db, err := internal.ConnectDatabase(logger)
	if err != nil {
		logger.Error("database initialization failed", "component", logging.ComponentDatabase, "error", err)
		return err
	}
	logger.Info("database ready", "component", logging.ComponentDatabase)

	queueService, err := queue.NewQueue(ctx, config, logger)
	if err != nil {
		logger.Error("queue initialization failed", "component", logging.ComponentQueue, "backend", "sqs", "error", err)
		return err
	}
	if err = queueService.Ready(ctx); err != nil {
		logger.Error("queue readiness check failed", "component", logging.ComponentQueue, "backend", "sqs", "error", err)
		return err
	}
	logger.Info("queue ready", "component", logging.ComponentQueue, "backend", "sqs")

	notifier, err := notification.NewNotifier(ctx, config, logger)
	if err != nil {
		logger.Error("notifier readiness check failed", "component", logging.ComponentNotifier, "backend", "ses", "error", err)
		return err
	}
	logger.Info("notifier ready", "component", logging.ComponentNotifier, "backend", "ses")

	ticketRepository := job.NewTicketRepository(db)

	httpClient := &http.Client{Timeout: time.Duration(60) * time.Second}
	adapter := clients.NewAdapterClient(httpClient, logger, config.GetMaximumAdapterResponseSizeBytes())
	defer httpClient.CloseIdleConnections()

	numberWorkers := runtime.NumCPU()
	wg := &sync.WaitGroup{}

	workerErrors := make([]error, numberWorkers)
	logger.Info(
		"worker pool starting",
		"component", logging.ComponentWorker,
		"worker_count", numberWorkers,
		"worker_timeout_seconds", config.GetWorkerTimeoutSeconds(),
	)

	for i := 0; i < numberWorkers; i++ {
		wg.Go(func() {
			worker := worker.NewBatchWorker(config, ticketRepository, queueService, storage, adapter, logger, notifier, serviceMetrics)

			workerCtx := logging.WithWorkerID(ctx, worker.GetID())
			workerLogger := logger.WithContext(workerCtx)

			processingItem, err := startWork(workerCtx, worker, config, workerLogger)

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
		logger.Error(
			"worker pool failed",
			"component", logging.ComponentWorker,
			"worker_count", numberWorkers,
			"error", err,
		)
		return err
	}

	logger.Info("worker pool completed", "component", logging.ComponentWorker, "worker_count", numberWorkers)
	return nil
}

func startWork(ctx context.Context, w worker.Worker, c config.Config, l logging.Logger) (bool, error) {
	wr := worker.NewWorkReport()

	defer func() {
		l.Info(
			"worker completed",
			"component", logging.ComponentWorker,
			"worker_id", w.GetID(),
			"ticket_id", wr.GetTicketID(),
			"polling_duration_ms", wr.GetPollingDurationMs(),
			"processing_duration_ms", wr.GetProcessingTimeMs(),
			"overall_duration_ms", wr.GetOverallTimeMs(),
			"processed_item", wr.GetProcessedItem(),
			"error", wr.GetError(),
		)
	}()

	workerTimeout := time.Duration(c.GetWorkerTimeoutSeconds()) * time.Second
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(workerTimeout))
	defer cancel()

	dequeueResult, err := w.Polling(ctx)
	wr.MarkPollingTime()
	if err != nil {
		l.Debug(
			"worker returned error during polling",
			"component", logging.ComponentWorker,
			"worker_id", w.GetID(),
			"error", err,
		)
		wr.SetError(err)
		return wr.GetProcessedItem(), err
	}
	wr.MarkProcessedItem()
	wr.SetTicketID(dequeueResult.Result.TicketID)

	ctx = logging.WithRequestID(ctx, dequeueResult.Result.RequestID)
	ctx = logging.WithTicketID(ctx, dequeueResult.Result.TicketID)
	l = l.WithContext(ctx)

	err = w.Start(ctx, *dequeueResult)
	wr.MarkProcessingTime()
	if err != nil {
		l.Debug(
			"worker returned error during processing",
			"component", logging.ComponentWorker,
			"worker_id", w.GetID(),
			"ticket_id", wr.GetTicketID(),
			"error", err,
		)
		wr.SetError(err)
		return wr.GetProcessedItem(), err
	}

	return wr.GetProcessedItem(), nil
}
