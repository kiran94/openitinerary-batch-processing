package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/di"
	oibplogging "github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/worker"
	"github.com/kiran94/openitinerary-batch-processing/pkg/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	ctx, interruptCancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer interruptCancel()

	config := config.NewConfig()
	bootstrapLogger := logging.New(config)

	services, err := di.NewServices(ctx, config.GetOtelWorkerName(), bootstrapLogger, config)
	if err != nil {
		return err
	}
	defer services.Shutdown()

	workerTracer := services.GetTracer("github.com/kiran94/openitinerary-batch-processing/cmd/worker")
	numberWorkers := runtime.NumCPU()
	wg := &sync.WaitGroup{}

	workerErrors := make([]error, numberWorkers)
	services.Logger.Info(ctx,
		"worker pool starting",
		"component", oibplogging.ComponentWorker,
		"worker_count", numberWorkers,
		"worker_timeout_seconds", config.GetWorkerTimeoutSeconds(),
	)

	for i := 0; i < numberWorkers; i++ {
		wg.Go(func() {
			worker := worker.NewBatchWorker(services.Config, services.TicketRepository, services.Queue, services.Storage, services.Adapter, services.Logger, services.Notifier, services.WorkerMetrics, workerTracer)

			workerCtx := logging.WithWorkerID(ctx, worker.GetID())
			workerLogger := services.Logger.WithContext(workerCtx)

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
		services.Logger.Error(ctx,
			"worker pool failed",
			"component", oibplogging.ComponentWorker,
			"worker_count", numberWorkers,
			"error", err,
		)
		return err
	}

	services.Logger.Info(ctx, "worker pool completed", "component", oibplogging.ComponentWorker, "worker_count", numberWorkers)
	return nil
}

func startWork(ctx context.Context, w worker.Worker, c config.Config, l logging.Logger) (bool, error) {
	wr := worker.NewWorkReport()

	workerTimeout := time.Duration(c.GetWorkerTimeoutSeconds()) * time.Second
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(workerTimeout))
	defer cancel()
	logger := l.WithContext(ctx)

	defer func() {
		logger.Info(ctx,
			"worker completed",
			"component", oibplogging.ComponentWorker,
			"polling_duration_ms", wr.GetPollingDurationMs(),
			"processing_duration_ms", wr.GetProcessingTimeMs(),
			"overall_duration_ms", wr.GetOverallTimeMs(),
			"processed_item", wr.GetProcessedItem(),
			"error", wr.GetError(),
		)
	}()

	dequeueResult, err := w.Polling(ctx)
	wr.MarkPollingTime()
	if err != nil {
		logger.Debug(ctx,
			"worker returned error during polling",
			"component", oibplogging.ComponentWorker,
			"error", err,
		)
		wr.SetError(err)
		return wr.GetProcessedItem(), err
	}
	wr.MarkProcessedItem()
	wr.SetTicketID(dequeueResult.Result.TicketID)

	ctx = logging.WithRequestID(ctx, dequeueResult.Result.RequestID)
	ctx = logging.WithTicketID(ctx, dequeueResult.Result.TicketID)
	logger = logger.WithContext(ctx)

	if telemetryCarrier, err := dequeueResult.GetTelemetryCarrier(); err != nil {
		logger.Warn(ctx, "telemetry headers were not correct", "error", err)
	} else {
		remoteCtx := otel.GetTextMapPropagator().Extract(ctx, telemetryCarrier)
		spanCtx := trace.SpanContextFromContext(remoteCtx)
		if !spanCtx.IsValid() || !spanCtx.IsRemote() {
			logger.Warn(ctx, "telemetry headers were not correct", "error", errors.New("span context was not remote or valid"))
		} else {
			ctx = remoteCtx
		}
	}

	err = w.Start(ctx, *dequeueResult)
	wr.MarkProcessingTime()
	if err != nil {
		logger.Debug(ctx,
			"worker returned error during processing",
			"component", oibplogging.ComponentWorker,
			"error", err,
		)
		wr.SetError(err)
		return wr.GetProcessedItem(), err
	}

	return wr.GetProcessedItem(), nil
}
