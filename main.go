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
)

func main() {
	ctx := context.Background()
	appConfig := config.ApplicationConfig{}
	logger := logging.New(appConfig)

	db, err := internal.ConnectDatabase(logger)
	if err != nil {
		logger.Error("database initialization failed", "component", logging.ComponentDatabase, "error", err)
		os.Exit(1)
	}
	logger.Info("database ready", "component", logging.ComponentDatabase)

	s3, err := storage.NewAWSS3Storage(ctx)
	if err != nil {
		logger.Error("storage initialization failed", "component", logging.ComponentStorage, "backend", "s3", "error", err)
		os.Exit(1)
	}
	logger.Info("storage ready", "component", logging.ComponentStorage, "backend", "s3")

	batchQueue, err := queue.NewQueue(ctx, appConfig, logger)
	if err != nil {
		logger.Error("queue initialization failed", "component", logging.ComponentQueue, "backend", "sqs", "error", err)
		os.Exit(1)
	}
	logger.Info("queue ready", "component", logging.ComponentQueue, "backend", "sqs")

	service := ServiceV1{
		config:  appConfig,
		job:     job.NewJobService(db),
		logger:  logger,
		queue:   batchQueue,
		storage: s3,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/submit", service.submit)
	mux.HandleFunc("GET /api/v1/status/{id}", service.status)

	server := &http.Server{
		Handler:      mux,
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
		os.Exit(1)
	}
	logger.Info("server stopped", "component", logging.ComponentHTTP)
}
