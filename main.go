package main

import (
	"context"
	"log"
	"net/http"

	"github.com/kiran94/openitinerary-batch-processing/internal"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
)

func main() {
	db, err := internal.ConnectDatabase()
	if err != nil {
		panic(err)
	}

	s3, err := storage.NewAWSS3Storage(context.Background())
	if err != nil {
		panic(err)
	}

	service := ServiceV1{
		config:  internal.ApplicationConfig{},
		job:     job.NewJobService(db),
		queue:   queue.NewInMemoryQueue(),
		storage: s3,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/submit", service.submit)
	mux.HandleFunc("GET /api/v1/status/{id}", service.status)

	const Port = ":8080"
	server := &http.Server{
		Handler:      mux,
		Addr:         service.config.GetPort(),
		ReadTimeout:  service.config.GetServerReadTimeout(),
		WriteTimeout: service.config.GetServerWriteTimeout(),
		IdleTimeout:  service.config.GetServerIdleTimeout(),
	}

	log.Printf("listening on port: '%v' \n", Port)
	err = server.ListenAndServe()
	if err != nil {
		panic(err)
	}
}
