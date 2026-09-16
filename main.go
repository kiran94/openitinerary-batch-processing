package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kiran94/openitinerary-batch-processing/internal"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/health"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/service"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Together these values will control how long it takes the server to shutdown
// in a worse case scenario so be careful when adjusting them.
const (
	shutdownTelemetryTimeout = 15 * time.Second
	shutdownServerTimeout    = 15 * time.Second
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	interruptCtx, interruptCancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer interruptCancel()

	appConfig := config.ApplicationConfig{}
	bootstrapLogger := logging.New(appConfig)

	telemetryManager, err := telemetry.NewTelemetryManager(ctx, appConfig.GetOtelServiceName(), appConfig)
	if err != nil {
		bootstrapLogger.Error(ctx, "otel initialization failed", "error", err)
		return err
	}
	defer shutdownTelemetry(telemetryManager, bootstrapLogger)

	if err := telemetryManager.Ready(); err != nil {
		bootstrapLogger.Error(ctx, "otel ready failed", "error", err)
		return err
	}

	serviceMetrics, err := telemetry.NewApplicationOtelMetrics(telemetryManager.GetMeterProvider())
	if err != nil {
		bootstrapLogger.Error(ctx, "metrics initialization failed", "error", err)
		return err
	}
	bootstrapLogger.Info(ctx, "telemetry ready", "component", logging.ComponentTelemetry)

	traceProvider := telemetryManager.GetTracerProvider()
	serviceTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing")
	storageTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing/internal/storage")
	queueTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing/internal/queue")
	jobTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing/internal/job")
	submitServiceTracer := traceProvider.Tracer("github.com/kiran94/openitinerary-batch-processing/internal/service")

	logger := logging.New(appConfig)
	telemetry.SetLogger(logger)

	db, err := internal.ConnectDatabase(ctx, logger, appConfig)
	if err != nil {
		logger.Error(ctx, "database initialization failed", "component", logging.ComponentDatabase, "error", err)
		return err
	}
	logger.Info(ctx, "database ready", "component", logging.ComponentDatabase)

	s3, err := storage.NewAWSS3Storage(ctx, appConfig, storageTracer)
	if err != nil {
		logger.Error(ctx, "storage initialization failed", "component", logging.ComponentStorage, "backend", "s3", "error", err)
		return err
	}
	logger.Info(ctx, "storage ready", "component", logging.ComponentStorage, "backend", "s3")

	batchQueue, err := queue.NewQueue(ctx, appConfig, logger, queueTracer)
	if err != nil {
		logger.Error(ctx, "queue initialization failed", "component", logging.ComponentQueue, "backend", "sqs", "error", err)
		return err
	}
	logger.Info(ctx, "queue ready", "component", logging.ComponentQueue, "backend", "sqs")

	internalDB, err := db.DB()
	if err != nil {
		logger.Error(ctx, "could not get internal db", "component", logging.ComponentDatabase, "error", err)
		return err
	}

	jobService := job.NewTicketRepository(db, jobTracer)

	srm := health.NewServiceReadyManager(appConfig, logger, internalDB)
	srm.Start(interruptCtx)

	service := ServiceV1{
		config:        appConfig,
		job:           jobService,
		logger:        logger,
		storage:       s3,
		metrics:       serviceMetrics,
		tracer:        serviceTracer,
		submitService: service.NewSubmitService(appConfig, logger, submitServiceTracer, s3, serviceMetrics, jobService, batchQueue),
		resultService: service.NewResultService(appConfig, logger, submitServiceTracer, jobService, s3),
		statusService: service.NewStatusService(jobService, submitServiceTracer),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/submit", service.submit)
	mux.HandleFunc("GET /api/v1/status/{id}", service.status)
	mux.HandleFunc("GET /api/v1/result/{id}", service.result)
	mux.HandleFunc("GET /health/live", srm.Live())
	mux.HandleFunc("GET /health/ready", srm.Ready())

	server := &http.Server{
		Handler: logging.RequestIDMiddleware(
			otelhttp.NewHandler(
				logging.RequestIDSpanMiddleware(mux), "api", otelhttp.WithFilter(telemetry.FilterRequests),
			),
		),
		Addr:         service.config.GetPort(),
		ReadTimeout:  service.config.GetServerReadTimeout(),
		WriteTimeout: service.config.GetServerWriteTimeout(),
		IdleTimeout:  service.config.GetServerIdleTimeout(),
	}

	logger.Info(ctx,
		"server listening",
		"component", logging.ComponentHTTP,
		"listen_addr", server.Addr,
		"environment", appConfig.GetEnvironment(),
		"read_timeout", server.ReadTimeout.String(),
		"write_timeout", server.WriteTimeout.String(),
		"idle_timeout", server.IdleTimeout.String(),
	)

	errorCh := make(chan error, 1)
	go func() {
		errorCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errorCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error(ctx, "server stopped unexpectedly", "component", logging.ComponentHTTP, "error", err)
			return err
		}
	case <-interruptCtx.Done():
		logger.Info(ctx, "server cancellation was signaled", "component", logging.ComponentHTTP, "timeout_seconds", shutdownServerTimeout.Seconds())

		shutdownCtx := context.Background()
		shutdownCtx, shutdownCancel := context.WithTimeout(shutdownCtx, shutdownServerTimeout)
		defer shutdownCancel()

		srm.Shutdown()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error(shutdownCtx, "error shutting down server", "component", logging.ComponentHTTP, "error", err)
			return err
		}
	}

	return nil
}

func shutdownTelemetry(tm *telemetry.TelemetryManager, l logging.Logger) {
	ctx := context.Background()

	l.Debug(ctx, "shutting down telemetry", "timeout_seconds", shutdownTelemetryTimeout.Seconds())
	cancelCtx, cancelFunc := context.WithTimeout(ctx, shutdownTelemetryTimeout)
	defer cancelFunc()

	if err := tm.Close(cancelCtx); err != nil {
		l.Error(cancelCtx, "failed to cleanup otel", "error", err)
	}
}
