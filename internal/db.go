package internal

import (
	"context"
	"log/slog"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/plugin/opentelemetry/tracing"
)

func ConnectDatabase(ctx context.Context, logger logging.Logger, cfg config.Config) (*gorm.DB, error) {
	databaseDSN := cfg.GetDatabaseConnection()
	db, err := gorm.Open(postgres.Open(databaseDSN), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&job.Ticket{}); err != nil {
		return nil, err
	}

	options := []tracing.Option{}
	if cfg.GetLoggingLevel() != slog.LevelDebug {
		options = append(options, tracing.WithoutQueryVariables())
	}

	if err := db.Use(tracing.NewPlugin(options...)); err != nil {
		return nil, err
	}

	return db, nil
}
