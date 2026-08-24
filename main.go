package main

import (
	"context"
	"encoding/json"
	"fmt"
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
		db:            db,
		job:           job.NewJobService(db),
		queue:         queue.NewInMemoryQueue(),
		storage:       s3,
		storageConfig: storage.NewStorageConfiguration(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/submit", service.submit)

	const Port = ":8080"
	log.Printf("listening on port: '%v' \n", Port)
	fmt.Println(http.ListenAndServe(Port, mux))
}

type ServiceV1 struct {
	db            job.Database
	job           job.JobCreator
	queue         queue.BatchQueue
	storage       storage.Storage
	storageConfig storage.StorageConfiguration
}

type ErrorResponse struct {
	Detail string `json:"detail"`
}

// @Summary      Submit a Document for Processing
// @Description  Receive a Document and return a receipt back to the user
// @Tags         batch
// @Accept       multipart/form-data
// @Produce      json
// @Param        file  formData  file  true  "Document to process"
// @Success      202  {object}  job.Ticket
// @Failure      400  {object}  ErrorResponse
// @Failure      415  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Router       /api/v1/submit [post]
func (s *ServiceV1) submit(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	r.Body = http.MaxBytesReader(w, r.Body, 10<<20) // 10MB

	if err := r.ParseMultipartForm(5 << 20); err != nil { // 5MB
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, uploadedFileHeader, err := r.FormFile("file")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	contentType := uploadedFileHeader.Header.Get("Content-Type")
	if contentType == "" {
		http.Error(w, "Content-Type must be provided", http.StatusBadRequest)
		return
	}

	var fileType string
	switch contentType {
	case "application/pdf":
		fileType = "pdf"
	case "text/html":
		fileType = "html"
	case "message/rfc822":
		fileType = "eml"
	default:
		http.Error(w, fmt.Sprintf("unsupported file format: %v", contentType), http.StatusBadRequest)
		return
	}

	ticket := job.NewTicket()
	ticket.DocumentPath = s.storageConfig.GetDocumentPath(ticket.ID.String(), fileType)
	ticket.ContentType = contentType

	if err := s.storage.Write(r.Context(), file, ticket.DocumentPath); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.job.Create(ticket); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.queue.Enqueue(ticket); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(ticket); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// func (s *ServiceV1) status(w http.ResponseWriter, r *http.Request) {

// }

// func main() {

// 	db, err := internal.ConnectDatabase()
// 	if err != nil {
// 		panic(err)
// 	}

// 	ticket := job.NewTicket("s3://thebucketofkiran456/openitinerary/documents/hello.pdf")

// 	jobService := job.NewJobService(db)
// 	ticketCreateErr := jobService.Create(ticket)

// 	if ticketCreateErr != nil {
// 		panic(ticketCreateErr)
// 	}
// path := "/Users/kiran/Documents/projects/open_itinerary/data/example_documents/Hotel_Brisbane.pdf"
// ctx := context.Background()

// file, err := os.Open(path)
// if err != nil {
// 	panic(err)
// }
// defer file.Close()

// http := &http.Client{
// 	Timeout: time.Duration(60) * time.Second,
// }
// client := clients.NewAdapterClient(http)

// reader, writer := io.Pipe()
// defer reader.Close()

// // Since the conversion runs within a Go routine
// // we use this channel to inspect the error
// // after the conversion is done back on the main thread.
// convertDone := make(chan error)
// go func() {
// 	err = client.Convert(ctx, file, filepath.Base(path), "application/pdf", writer)
// 	convertDone <- err
// }()

// destinationFile := "s3://thebucketofkiran456/openitinerary/output.json"
// destination, err := storage.NewAWSS3Storage(ctx)
// if err != nil {
// 	panic(err)
// }
// fmt.Printf("Writing data to %v \n", destinationFile)

// copyErr := destination.Write(ctx, reader, destinationFile)
// if copyErr != nil {
// 	_ = reader.Close()
// }
// convertError := <-convertDone

// if err := errors.Join(convertError, copyErr); err != nil {
// 	panic(err)
// }
// }
