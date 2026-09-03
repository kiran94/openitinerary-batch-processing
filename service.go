package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry/metrics"
)

type ServiceV1 struct {
	config  config.Config
	job     job.TicketRepository
	logger  logging.Logger
	queue   queue.BatchQueue
	storage storage.Storage
	metrics telemetry.ServiceMetrics
}

// @Summary      Submit a document for processing
// @Description  Upload a PDF, HTML, or RFC 822 email document, create and enqueue a processing ticket, and return the accepted ticket.
// @Tags         batch
// @Accept       multipart/form-data
// @Produce      json
// @Param        file  formData  file  true  "Document to process"
// @Success      202  {object}  job.Ticket
// @Failure      400  {object}  apiError  "Invalid multipart form, missing file, or missing Content-Type"
// @Failure      413  {object}  apiError  "Request body exceeds the configured limit"
// @Failure      415  {object}  apiError  "Invalid or unsupported media type"
// @Failure      500  {object}  apiError  "Document submission failed"
// @Router       /api/v1/submit [post]
func (s *ServiceV1) submit(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now().UTC()
	defer r.Body.Close()

	r.Body = http.MaxBytesReader(w, r.Body, s.config.GetMaximumAllowedRequestSizeMb())

	if err := r.ParseMultipartForm(s.config.GetMaximumBufferMultipartForm()); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			s.writeAPIError(w, http.StatusRequestEntityTooLarge, "request body is too large")
		} else {
			s.writeAPIError(w, http.StatusBadRequest, "invalid multipart form")
		}

		return
	}
	defer func() {
		err := r.MultipartForm.RemoveAll()
		if err != nil {
			s.logger.Warn("multipart form cleanup failed", "component", logging.ComponentHTTP, "operation", "multipart_cleanup", "error", err)
		}
	}()

	file, uploadedFileHeader, err := r.FormFile("file")
	if err != nil {
		s.writeAPIError(w, http.StatusBadRequest, "file must be provided")
		return
	}
	defer file.Close()

	contentType := uploadedFileHeader.Header.Get("Content-Type")
	if contentType == "" {
		s.writeAPIError(w, http.StatusBadRequest, "file content type must be provided")
		return
	}
	contentType, _, err = mime.ParseMediaType(contentType)
	if err != nil {
		s.writeAPIError(w, http.StatusUnsupportedMediaType, "file content type is invalid")
		return
	}

	emailRaw := r.Form.Get("notification_emails")
	var emails []string
	if err := json.Unmarshal([]byte(emailRaw), &emails); err != nil {
		s.writeAPIError(w, http.StatusBadRequest, "notification_emails must be a JSON array of strings")
		return
	}

	notificationEmailLimit := s.config.GetNotificationEmailLimit()
	if len(emails) > notificationEmailLimit {
		s.writeAPIError(
			w,
			http.StatusBadRequest,
			fmt.Sprintf("notification_emails must contain at most %v entries", notificationEmailLimit),
		)
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
		s.writeAPIError(
			w,
			http.StatusUnsupportedMediaType,
			fmt.Sprintf("unsupported file content type: %v", contentType),
		)
		return
	}

	ticket := job.NewTicket()
	ticket.DocumentPath = s.config.GetDocumentPath(s.config.GetBucket(), s.config.GetBucketPath(), ticket.ID.String(), fileType)
	ticket.ContentType = contentType
	ticket.NotificationEmails = emails

	storageStart := time.Now()
	if err := s.storage.Write(r.Context(), file, ticket.DocumentPath); err != nil {
		s.metrics.RecordStorageUpload(
			r.Context(),
			metrics.StorageUploadObservation{
				Duration: time.Since(storageStart),
				Purpose:  metrics.StorageUploadPurposeDocument,
				Outcome:  metrics.StorageUploadFailed,
			},
		)
		s.logger.Error("document storage failed", "component", logging.ComponentStorage, "ticket_id", ticket.ID.String(), "content_type", contentType, "error", err)
		s.writeAPIError(w, http.StatusInternalServerError, "failed to submit document")
		return
	}
	s.metrics.RecordStorageUpload(
		r.Context(),
		metrics.StorageUploadObservation{
			Duration: time.Since(storageStart),
			Purpose:  metrics.StorageUploadPurposeDocument,
			Outcome:  metrics.StorageUploadSuccess,
		},
	)

	if err := s.job.Create(r.Context(), ticket); err != nil {
		s.logger.Error("ticket creation failed", "component", logging.ComponentDatabase, "ticket_id", ticket.ID.String(), "error", err)
		s.writeAPIError(w, http.StatusInternalServerError, "failed to submit document")
		return
	}

	if err := s.queue.Enqueue(r.Context(), ticket); err != nil {
		detail := fmt.Sprintf("enqueue failed. err: %v", err)
		updated, updateErr := s.job.MarkFailed(r.Context(), ticket.ID, job.EnqueueFailed, detail)
		if updateErr != nil {
			s.logger.Error("ticket failure update failed", "component", logging.ComponentDatabase, "ticket_id", ticket.ID.String(), "reason", job.EnqueueFailed, "error", updateErr)
		} else if !updated {
			s.logger.Warn("ticket failure update skipped", "component", logging.ComponentDatabase, "ticket_id", ticket.ID.String(), "reason", job.EnqueueFailed)
		} else {
			s.logger.Info("ticket marked failed", "component", logging.ComponentDatabase, "ticket_id", ticket.ID.String(), "reason", job.EnqueueFailed)
		}

		s.logger.Error("queue enqueue failed", "component", logging.ComponentQueue, "ticket_id", ticket.ID.String(), "error", err)
		s.writeAPIError(w, http.StatusInternalServerError, "failed to submit document")
		return
	}

	responseBody, err := json.Marshal(ticket)
	if err != nil {
		s.logger.Error("submit response serialization failed", "component", logging.ComponentHTTP, "ticket_id", ticket.ID.String(), "error", err)
		s.writeAPIError(w, http.StatusInternalServerError, "failed to submit document")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)

	if _, err := w.Write(responseBody); err != nil {
		s.logger.Warn("response write failed", "component", logging.ComponentHTTP, "ticket_id", ticket.ID.String(), "status_code", http.StatusAccepted, "error", err)
		return
	}

	s.logger.Info("submit accepted", "ticket_id", ticket.ID.String(), "content_type", contentType, "status", ticket.Status, "status_code", http.StatusAccepted, "duration_ms", time.Since(startedAt).Milliseconds())
}

// status retrieves the current processing state of a ticket.
// @Summary      Get ticket status
// @Description  Retrieve the current processing state of a ticket by its UUID.
// @Tags         batch
// @Produce      json
// @Param        id  path      string      true  "Ticket UUID"
// @Success      200 {object}  job.Ticket
// @Failure      400 {object}  apiError   "Missing or invalid ticket UUID"
// @Failure      404 {object}  apiError   "Ticket not found"
// @Failure      500 {object}  apiError   "Failed to retrieve ticket status"
// @Router       /api/v1/status/{id} [get]
func (s *ServiceV1) status(w http.ResponseWriter, r *http.Request) {

	ticketID := r.PathValue("id")
	if ticketID == "" {
		s.writeAPIError(w, http.StatusBadRequest, "id parameter must be provided")
		return
	}

	ticketIDParsed, err := uuid.Parse(ticketID)
	if err != nil {
		s.writeAPIError(w, http.StatusBadRequest, "id must be a valid UUID")
		return
	}

	ticket, err := s.job.Get(r.Context(), ticketIDParsed)
	if err != nil {
		if errors.Is(err, job.ErrRecordNotFound) {
			s.writeAPIError(w, http.StatusNotFound, "ticket was not found")
		} else {
			s.logger.Error("ticket status lookup failed", "component", logging.ComponentDatabase, "ticket_id", ticketIDParsed.String(), "error", err)
			s.writeAPIError(w, http.StatusInternalServerError, "failed to retrieve ticket status")
		}
		return
	}

	responseBody, err := json.Marshal(ticket)
	if err != nil {
		s.logger.Error("ticket status response serialization failed", "component", logging.ComponentHTTP, "ticket_id", ticket.ID.String(), "error", err)
		s.writeAPIError(w, http.StatusInternalServerError, "failed to retrieve ticket status")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(responseBody); err != nil {
		s.logger.Warn("response write failed", "component", logging.ComponentHTTP, "ticket_id", ticket.ID.String(), "status_code", http.StatusOK, "error", err)
	}
}

type resultOutput struct {
	Link    string    `json:"link"`
	Expires time.Time `json:"expires"`
}

// result returns a temporary download link for a successfully completed ticket.
// @Summary      Get processing result
// @Description  Generate a temporary download link for the result of a successfully completed ticket.
// @Tags         batch
// @Produce      json
// @Param        id  path      string        true  "Ticket UUID"
// @Success      200 {object}  resultOutput  "Temporary result download link"
// @Success      204 "Ticket has not successfully completed"
// @Failure      400 {object}  apiError     "Missing or invalid ticket UUID"
// @Failure      404 {object}  apiError     "Ticket not found"
// @Failure      500 {object}  apiError     "Failed to retrieve the processing result"
// @Router       /api/v1/result/{id} [get]
func (s *ServiceV1) result(w http.ResponseWriter, r *http.Request) {

	ticketID := r.PathValue("id")
	if ticketID == "" {
		s.writeAPIError(w, http.StatusBadRequest, "id parameter must be provided")
		return
	}

	ticketIDParsed, err := uuid.Parse(ticketID)
	if err != nil {
		s.writeAPIError(w, http.StatusBadRequest, "id must be a valid UUID")
		return
	}

	ticket, err := s.job.Get(r.Context(), ticketIDParsed)
	if err != nil {
		if errors.Is(err, job.ErrRecordNotFound) {
			s.writeAPIError(w, http.StatusNotFound, "ticket was not found")
		} else {
			s.logger.Error("ticket status lookup failed", "component", logging.ComponentDatabase, "ticket_id", ticketIDParsed.String(), "error", err)
			s.writeAPIError(w, http.StatusInternalServerError, "failed to retrieve processing result")
		}
		return
	}

	if ticket.Status != job.Complete {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	outputPath := s.config.GetOutputPath(s.config.GetBucket(), s.config.GetBucketPath(), ticket.ID.String())

	link, expiration, err := s.storage.GetLink(r.Context(), outputPath)
	if err != nil {
		s.logger.Error("failed to generate presigned link", "component", logging.ComponentStorage, "ticket_id", ticketIDParsed.String(), "error", err)
		s.writeAPIError(w, http.StatusInternalServerError, "failed to retrieve processing result")
		return
	}

	result := resultOutput{
		Link:    link,
		Expires: expiration,
	}

	b, err := json.Marshal(result)
	if err != nil {
		s.logger.Error("failed to marshal presigned result obj", "component", logging.ComponentStorage, "ticket_id", ticketIDParsed.String(), "error", err)
		s.writeAPIError(w, http.StatusInternalServerError, "failed to retrieve processing result")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(b); err != nil {
		s.logger.Error("failed to write response", "component", logging.ComponentHTTP, "ticket_id", ticketIDParsed.String(), "status_code", http.StatusOK, "error", err)
	}
}

type apiError struct {
	Detail string `json:"detail"`
}

func (s *ServiceV1) writeAPIError(w http.ResponseWriter, statusCode int, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(apiError{Detail: detail}); err != nil {
		s.logger.Warn(
			"error response write failed",
			"component", logging.ComponentHTTP,
			"status_code", statusCode,
			"error", err,
		)
	}
}
