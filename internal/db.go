package internal

import (
	"log/slog"
	"os"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/plugin/opentelemetry/tracing"
)

func ConnectDatabase(logger logging.Logger, cfg config.Config) (*gorm.DB, error) {
	DATABASE_DSN := os.Getenv("DATABASE_DSN")
	if DATABASE_DSN == "" {
		logger.Warn("database dsn defaulted", "component", logging.ComponentDatabase, "dsn_source", "default")
		DATABASE_DSN = "host=localhost user=openitinerary password=openitinerary dbname=openitinerary_batch_processing port=5432 sslmode=disable"
	}

	db, err := gorm.Open(postgres.Open(DATABASE_DSN), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&job.Ticket{}); err != nil {
		return nil, err
	}

	options := []tracing.Option{}
	if cfg.GetLoggingLevel() == slog.LevelDebug {
		options = append(options, tracing.WithoutQueryVariables())
	}

	if err := db.Use(tracing.NewPlugin(options...)); err != nil {
		return nil, err
	}

	return db, nil
}
