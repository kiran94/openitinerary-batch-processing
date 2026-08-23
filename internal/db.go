package internal

import (
	"log"
	"os"

	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func ConnectDatabase() (*gorm.DB, error) {
	DATABASE_DSN := os.Getenv("DATABASE_DSN")
	if DATABASE_DSN == "" {
		log.Printf("using default database dsn")
		DATABASE_DSN = "host=localhost user=openitinerary password=openitinerary dbname=openitinerary_batch_processing port=5432 sslmode=disable"
	}

	db, err := gorm.Open(postgres.Open(DATABASE_DSN), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&job.Ticket{}); err != nil {
		return nil, err
	}

	return db, nil
}
