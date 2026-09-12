package main

import (
	"context"
	"errors"
	"net/http"
	"os"

	"github.com/kiran94/openitinerary-batch-processing/internal"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	appConfig := config.ApplicationConfig{}
	bootstrapLogger := logging.New(appConfig)

	telemetryManager, err := telemetry.NewTelemetryManager(ctx, appConfig.GetOtelServiceName(), appConfig)
	if err != nil {
		bootstrapLogger.Error("otel initialization failed", "error", err)
		return err
	}
	if err := telemetryManager.Ready(); err != nil {
		bootstrapLogger.Error("otel ready failed", "error", err)
		return err
	}

	defer func() {
		if err := telemetryManager.Close(ctx); err != nil {
			bootstrapLogger.Error("failed to cleanup otel", "error", err)
		}
	}()

	serviceMetrics, err := telemetry.NewApplicationOtelMetrics(telemetryManager.GetMeterProvider())
	if err != nil {
		bootstrapLogger.Error("metrics initialization failed", "error", err)
		return err
	}
	bootstrapLogger.Info("telemetry ready", "component", logging.ComponentTelemetry)

	traceProvider := telemetryManager.GetTracerProvider()
	serviceTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing")
	storageTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing/internal/storage")
	queueTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing/internal/queue")
	jobTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing/internal/job")

	logger := logging.New(appConfig)
	telemetry.SetLogger(logger)

	db, err := internal.ConnectDatabase(logger, appConfig)
	if err != nil {
		logger.Error("database initialization failed", "component", logging.ComponentDatabase, "error", err)
		return err
	}
	logger.Info("database ready", "component", logging.ComponentDatabase)

	s3, err := storage.NewAWSS3Storage(ctx, appConfig, storageTracer)
	if err != nil {
		logger.Error("storage initialization failed", "component", logging.ComponentStorage, "backend", "s3", "error", err)
		return err
	}
	logger.Info("storage ready", "component", logging.ComponentStorage, "backend", "s3")

	batchQueue, err := queue.NewQueue(ctx, appConfig, logger, queueTracer)
	if err != nil {
		logger.Error("queue initialization failed", "component", logging.ComponentQueue, "backend", "sqs", "error", err)
		return err
	}
	logger.Info("queue ready", "component", logging.ComponentQueue, "backend", "sqs")

	service := ServiceV1{
		config:  appConfig,
		job:     job.NewTicketRepository(db, jobTracer),
		logger:  logger,
		queue:   batchQueue,
		storage: s3,
		metrics: serviceMetrics,
		tracer:  serviceTracer,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/submit", service.submit)
	mux.HandleFunc("GET /api/v1/status/{id}", service.status)
	mux.HandleFunc("GET /api/v1/result/{id}", service.result)

	server := &http.Server{
		Handler: logging.RequestIDMiddleware(
			otelhttp.NewHandler(
				logging.RequestIDSpanMiddleware(mux), "api"),
		),
		Addr:         service.config.GetPort(),
		ReadTimeout:  service.config.GetServerReadTimeout(),
		WriteTimeout: service.config.GetServerWriteTimeout(),
		IdleTimeout:  service.config.GetServerIdleTimeout(),
	}

	logger.Info(
		"server listening",
		"component", logging.ComponentHTTP,
		"listen_addr", server.Addr,
		"environment", appConfig.GetEnvironment(),
		"read_timeout", server.ReadTimeout.String(),
		"write_timeout", server.WriteTimeout.String(),
		"idle_timeout", server.IdleTimeout.String(),
	)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server stopped unexpectedly", "component", logging.ComponentHTTP, "error", err)
		return err
	}

	logger.Info("server stopped", "component", logging.ComponentHTTP)
	return nil
}
