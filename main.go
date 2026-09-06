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
	logger := logging.New(appConfig)

	telemetry.SetLogger(logger)
	otelMetricProvider, err := telemetry.NewMeterManager(ctx, appConfig.GetOtelServiceName(), appConfig)
	if err != nil {
		logger.Error("otel initialization failed", "error", err)
		return err
	}
	if err := otelMetricProvider.Ready(); err != nil {
		logger.Error("otel ready failed", "error", err)
		return err
	}

	defer func() {
		if err := otelMetricProvider.Close(ctx); err != nil {
			logger.Error("failed to cleanup otel", "error", err)
		}
	}()

	serviceMetrics, err := telemetry.NewApplicationOtelMetrics(otelMetricProvider.GetProvider())
	if err != nil {
		logger.Error("metrics initialization failed", "error", err)
		return err
	}
	logger.Info("telemtry ready", "component", logging.ComponentTelemetry)

	db, err := internal.ConnectDatabase(logger)
	if err != nil {
		logger.Error("database initialization failed", "component", logging.ComponentDatabase, "error", err)
		return err
	}
	logger.Info("database ready", "component", logging.ComponentDatabase)

	s3, err := storage.NewAWSS3Storage(ctx, appConfig)
	if err != nil {
		logger.Error("storage initialization failed", "component", logging.ComponentStorage, "backend", "s3", "error", err)
		return err
	}
	logger.Info("storage ready", "component", logging.ComponentStorage, "backend", "s3")

	batchQueue, err := queue.NewQueue(ctx, appConfig, logger)
	if err != nil {
		logger.Error("queue initialization failed", "component", logging.ComponentQueue, "backend", "sqs", "error", err)
		return err
	}
	logger.Info("queue ready", "component", logging.ComponentQueue, "backend", "sqs")

	service := ServiceV1{
		config:  appConfig,
		job:     job.NewTicketRepository(db),
		logger:  logger,
		queue:   batchQueue,
		storage: s3,
		metrics: serviceMetrics,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/submit", service.submit)
	mux.HandleFunc("GET /api/v1/status/{id}", service.status)
	mux.HandleFunc("GET /api/v1/result/{id}", service.result)

	server := &http.Server{
		Handler:      logging.RequestIDMiddleware(otelhttp.NewHandler(mux, "api")),
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
