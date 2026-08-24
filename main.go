package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"

	"github.com/google/uuid"
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
		db:      db,
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

type ServiceV1 struct {
	config  internal.Config
	db      job.Database
	job     job.JobCreator
	queue   queue.BatchQueue
	storage storage.Storage
}

// @Summary      Submit a document for processing
// @Description  Upload a PDF, HTML, or RFC 822 email document, create and enqueue a processing ticket, and return the accepted ticket.
// @Tags         batch
// @Accept       multipart/form-data
// @Produce      json,plain
// @Param        file  formData  file  true  "Document to process"
// @Success      202  {object}  job.Ticket
// @Failure      400  {string}  string  "Invalid multipart form, missing file, or missing Content-Type"
// @Failure      413  {string}  string  "Request body exceeds 10 MiB"
// @Failure      415  {string}  string  "Invalid or unsupported media type"
// @Failure      500  {string}  string  "Document submission failed"
// @Router       /api/v1/submit [post]
func (s *ServiceV1) submit(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	r.Body = http.MaxBytesReader(w, r.Body, s.config.GetMaximumAllowedRequestSizeMb())

	if err := r.ParseMultipartForm(s.config.GetMaximumBufferMultipartForm()); err != nil {

		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request body is too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid multipart form", http.StatusBadRequest)
		}

		return
	}
	defer func() {
		err := r.MultipartForm.RemoveAll()
		if err != nil {
			log.Println(err.Error())
		}
	}()

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
	contentType, _, err = mime.ParseMediaType(contentType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnsupportedMediaType)
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
		http.Error(w, fmt.Sprintf("unsupported file format: %v", contentType), http.StatusUnsupportedMediaType)
		return
	}

	ticket := job.NewTicket()
	ticket.DocumentPath = s.config.GetDocumentPath(s.config.GetBucket(), s.config.GetBucketPath(), ticket.ID.String(), fileType)
	ticket.ContentType = contentType

	if err := s.storage.Write(r.Context(), file, ticket.DocumentPath); err != nil {
		obscureError(w, ticket, err, "submit")
		return
	}

	if err := s.job.Create(ticket); err != nil {
		obscureError(w, ticket, err, "submit")
		return
	}

	if err := s.queue.Enqueue(ticket); err != nil {
		obscureError(w, ticket, err, "submit")
		return
	}

	responseBody, err := json.Marshal(ticket)
	if err != nil {
		obscureError(w, ticket, err, "submit")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)

	if _, err := w.Write(responseBody); err != nil {
		log.Printf("failed to write response ticket_id=%s: %v", ticket.ID, err)
	}
}

func obscureError[T fmt.Stringer](w http.ResponseWriter, obj T, err error, errorSource string) {
	log.Printf("failed to perform operation '%v', object: '%v', err: '%v'", errorSource, obj, err)
	http.Error(w, fmt.Sprintf("failed to perform '%v'", errorSource), http.StatusInternalServerError)
}

func (s *ServiceV1) status(w http.ResponseWriter, r *http.Request) {

	ticketId := r.PathValue("id")
	if ticketId == "" {
		http.Error(w, "id parameter must be provided", http.StatusBadRequest)
		return
	}

	ticketIdParsed, err := uuid.Parse(ticketId)
	if err != nil {
		http.Error(w, fmt.Sprintf("provided id was not a valid guid. Got: '%v'", ticketId), http.StatusBadRequest)
		return
	}

	ticket, err := s.job.Get(ticketIdParsed)
	if err != nil {
		if errors.Is(err, job.ErrRecordNotFound) {
			http.Error(w, fmt.Sprintf("Ticket '%v' was not found", ticketId), http.StatusNotFound)
		} else {
			obscureError(w, ticketIdParsed, err, "status")
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(&ticket); err != nil {
		obscureError(w, ticket, err, "status")
		return
	}
}

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
