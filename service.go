package main

import (
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

type ServiceV1 struct {
	config  internal.Config
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

// status retrieves the current processing state of a ticket.
// @Summary      Get ticket status
// @Description  Retrieve the current processing state of a ticket by its UUID.
// @Tags         batch
// @Produce      json,plain
// @Param        id  path      string      true  "Ticket UUID"
// @Success      200 {object}  job.Ticket
// @Failure      400 {string}  string      "Missing or invalid ticket UUID"
// @Failure      404 {string}  string      "Ticket not found"
// @Failure      500 {string}  string      "Failed to retrieve ticket status"
// @Router       /api/v1/status/{id} [get]
func (s *ServiceV1) status(w http.ResponseWriter, r *http.Request) {

	ticketID := r.PathValue("id")
	if ticketID == "" {
		http.Error(w, "id parameter must be provided", http.StatusBadRequest)
		return
	}

	ticketIDParsed, err := uuid.Parse(ticketID)
	if err != nil {
		http.Error(w, fmt.Sprintf("provided id was not a valid guid. Got: '%v'", ticketID), http.StatusBadRequest)
		return
	}

	ticket, err := s.job.Get(ticketIDParsed)
	if err != nil {
		if errors.Is(err, job.ErrRecordNotFound) {
			http.Error(w, fmt.Sprintf("Ticket '%v' was not found", ticketID), http.StatusNotFound)
		} else {
			obscureError(w, ticketIDParsed, err, "status")
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(&ticket); err != nil {
		obscureError(w, ticket, err, "status")
		return
	}
}

func obscureError[T fmt.Stringer](w http.ResponseWriter, obj T, err error, errorSource string) {
	log.Printf("failed to perform operation '%v', object: '%v', err: '%v'", errorSource, obj, err)
	http.Error(w, fmt.Sprintf("failed to perform '%v'", errorSource), http.StatusInternalServerError)
}
