package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
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

type submitRequest struct {
	file               multipart.File
	fileHeader         *multipart.FileHeader
	contentType        string
	fileType           string
	metricContentType  metrics.ContentType
	notificationEmails []string
}

type submitRequestError struct {
	statusCode int
	detail     string
	reason     metrics.SubmissionReason
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

	ctx := r.Context()
	logger := s.logger.WithContext(ctx)

	defer func() {
		if r.MultipartForm == nil {
			return
		}

		err := r.MultipartForm.RemoveAll()
		if err != nil {
			logger.Warn("multipart form cleanup failed", "component", logging.ComponentHTTP, "operation", logging.OperationMultipartCleanup, "error", err)
		}
	}()

	request, requestErr := s.parseSubmitRequest(w, r)
	if request.file != nil {
		defer request.file.Close()
	}
	if requestErr != nil {
		s.writeAPIError(ctx, logger, w, metrics.EndpointSubmit, requestErr.statusCode, requestErr.detail)
		reason := requestErr.reason
		s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
			Outcome: metrics.SubmissionRejected,
			Reason:  &reason,
		})
		return
	}

	ticket := job.NewTicket()
	ticket.DocumentPath = s.config.GetDocumentPath(s.config.GetBucket(), s.config.GetBucketPath(), ticket.ID.String(), request.fileType)
	ticket.ContentType = string(request.metricContentType)
	ticket.NotificationEmails = request.notificationEmails

	ctx = logging.WithTicketID(ctx, ticket.ID.String())
	logger = logger.WithContext(ctx)

	if err := s.storeAndEnqueue(ctx, logger, request, ticket, startedAt); err != nil {
		s.writeAPIError(ctx, logger, w, metrics.EndpointSubmit, http.StatusInternalServerError, "failed to submit document")
		return
	}

	s.writeSubmitResponse(ctx, logger, w, request, ticket, startedAt)
}

func (s *ServiceV1) parseSubmitRequest(w http.ResponseWriter, r *http.Request) (submitRequest, *submitRequestError) {
	r.Body = http.MaxBytesReader(w, r.Body, s.config.GetMaximumAllowedRequestSizeMb())

	if err := r.ParseMultipartForm(s.config.GetMaximumBufferMultipartForm()); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return submitRequest{}, &submitRequestError{
				statusCode: http.StatusRequestEntityTooLarge,
				detail:     "request body is too large",
				reason:     metrics.SubmissionReasonRequestBodyTooLarge,
			}
		}

		return submitRequest{}, &submitRequestError{
			statusCode: http.StatusBadRequest,
			detail:     "invalid multipart form",
			reason:     metrics.SubmissionReasonInvalidMultipartForm,
		}
	}

	file, fileHeader, err := r.FormFile("file")
	request := submitRequest{file: file, fileHeader: fileHeader}
	if err != nil {
		return request, &submitRequestError{
			statusCode: http.StatusBadRequest,
			detail:     "file must be provided",
			reason:     metrics.SubmissionReasonFileNotProvided,
		}
	}

	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" {
		return request, &submitRequestError{
			statusCode: http.StatusBadRequest,
			detail:     "file content type must be provided",
			reason:     metrics.SubmissionReasonContentTypeNotProvided,
		}
	}
	contentType, _, err = mime.ParseMediaType(contentType)
	if err != nil {
		return request, &submitRequestError{
			statusCode: http.StatusUnsupportedMediaType,
			detail:     "file content type is invalid",
			reason:     metrics.SubmissionReasonInvalidContentType,
		}
	}

	emails, err := s.parseNotificationEmails(r)
	if err != nil {
		return request, &submitRequestError{
			statusCode: http.StatusBadRequest,
			detail:     err.Error(),
			reason:     metrics.SubmissionReasonInvalidNotificationEmails,
		}
	}

	fileType, metricContentType, err := contentTypeConversion(contentType)
	if err != nil {
		return request, &submitRequestError{
			statusCode: http.StatusUnsupportedMediaType,
			detail:     fmt.Sprintf("unsupported file content type: %s", contentType),
			reason:     metrics.SubmissionReasonUnsupportedContentType,
		}
	}

	request.contentType = contentType
	request.fileType = fileType
	request.metricContentType = metricContentType
	request.notificationEmails = emails
	return request, nil
}

func (s *ServiceV1) storeAndEnqueue(
	ctx context.Context,
	logger logging.Logger,
	request submitRequest,
	ticket *job.Ticket,
	startedAt time.Time,
) error {
	inputBytes := request.fileHeader.Size

	storageStart := time.Now()
	if err := s.storage.Write(ctx, request.file, ticket.DocumentPath); err != nil {
		s.metrics.RecordStorageUpload(
			ctx,
			metrics.StorageUploadObservation{
				Duration: time.Since(storageStart),
				Purpose:  metrics.StorageUploadPurposeDocument,
				Outcome:  metrics.StorageUploadFailed,
			},
		)
		s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
			Outcome:     metrics.SubmissionFailed,
			Reason:      new(metrics.SubmissionReasonStorageWriteFailed),
			ContentType: &request.metricContentType,
			InputBytes:  &inputBytes,
		})
		logger.Error("document storage failed", "component", logging.ComponentStorage, "operation", logging.OperationStorageWrite, "content_type", request.contentType, "error", err)
		return err
	}
	s.metrics.RecordStorageUpload(
		ctx,
		metrics.StorageUploadObservation{Duration: time.Since(storageStart), Purpose: metrics.StorageUploadPurposeDocument, Outcome: metrics.StorageUploadSuccess},
	)

	if err := s.job.Create(ctx, ticket); err != nil {
		logger.Error("ticket creation failed", "component", logging.ComponentDatabase, "operation", logging.OperationTicketCreate, "error", err)
		s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
			Outcome:     metrics.SubmissionFailed,
			Reason:      new(metrics.SubmissionReasonTicketCreationFailed),
			ContentType: &request.metricContentType,
			InputBytes:  &inputBytes,
		})
		return err
	}

	if err := s.queue.Enqueue(ctx, ticket); err != nil {
		detail := fmt.Sprintf("enqueue failed. err: %v", err)
		commonUpdateErrLoggingFields := []any{
			"component", string(logging.ComponentDatabase),
			"reason", string(job.EnqueueFailed),
			"original_error", err,
			"operation", logging.OperationMarkFailed,
		}

		updated, updateErr := s.job.MarkFailed(ctx, ticket.ID, job.EnqueueFailed, detail)
		if updateErr != nil {
			commonUpdateErrLoggingFields = append(commonUpdateErrLoggingFields, "error", updateErr)
			logger.Error("ticket failure update failed", commonUpdateErrLoggingFields...)
		} else if !updated {
			logger.Warn("ticket failure update skipped", commonUpdateErrLoggingFields...)
		} else {
			logger.Info("ticket marked failed", commonUpdateErrLoggingFields...)
		}

		logger.Error("queue enqueue failed", "component", logging.ComponentQueue, "error", err, "operation", logging.OperationEnqueue)
		s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
			Outcome:     metrics.SubmissionFailed,
			Reason:      new(metrics.SubmissionReasonQueueEnqueueFailed),
			ContentType: &request.metricContentType,
			InputBytes:  &inputBytes,
		})
		s.metrics.RecordJobTerminal(ctx, metrics.TerminalObservation{
			Outcome: metrics.TerminalFailed,
			Reason:  new(metrics.TerminalReasonEnqueueFailed),
		})
		s.metrics.RecordJobEndToEnd(ctx, metrics.JobEndObservation{
			Duration: time.Since(startedAt),
			Outcome:  metrics.JobEndOutcomeFailed,
			Reason:   new(metrics.TerminalReasonEnqueueFailed),
		})
		return err
	}

	return nil
}

func (s *ServiceV1) writeSubmitResponse(
	ctx context.Context,
	logger logging.Logger,
	w http.ResponseWriter,
	request submitRequest,
	ticket *job.Ticket,
	startedAt time.Time,
) {
	responseBody, err := json.Marshal(ticket)
	inputBytes := request.fileHeader.Size
	if err != nil {
		logger.Error("submit response serialization failed", "component", logging.ComponentHTTP, "operation", logging.OperationResponseSerialization, "error", err)
		s.writeAPIError(ctx, logger, w, metrics.EndpointSubmit, http.StatusInternalServerError, "failed to submit document")
		s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
			Outcome:     metrics.SubmissionFailed,
			Reason:      new(metrics.SubmissionReasonResponseSerializationFailed),
			ContentType: &request.metricContentType,
			InputBytes:  &inputBytes,
			Enqueued:    true,
		})
		return
	}

	s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
		Outcome:     metrics.SubmissionAccepted,
		Enqueued:    true,
		InputBytes:  &inputBytes,
		ContentType: &request.metricContentType,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)

	if _, err := w.Write(responseBody); err != nil {
		logger.Warn(
			"response write failed",
			"component", logging.ComponentHTTP,
			"status_code", http.StatusAccepted,
			"error", err,
			"operation", logging.OperationErrorResponseWrite,
		)
		s.metrics.RecordResponseWriteError(ctx, metrics.ResponseObservation{Endpoint: metrics.EndpointSubmit})
		return
	}

	logger.Info(
		"submit accepted",
		"content_type", request.contentType,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"component", logging.ComponentHTTP,
		"operation", logging.OperationSubmit,
	)
}

func contentTypeConversion(contentType string) (string, metrics.ContentType, error) {
	switch contentType {
	case "application/pdf":
		return "pdf", metrics.ContentTypePDF, nil
	case "text/html":
		return "html", metrics.ContentTypeHTML, nil
	case "message/rfc822":
		return "eml", metrics.ContentTypeEmail, nil
	default:
	}

	return "", "", fmt.Errorf("unsupported content type: %s", contentType)
}

func (s *ServiceV1) parseNotificationEmails(r *http.Request) ([]string, error) {
	emailRaw := r.Form.Get("notification_emails")

	var emails []string
	if err := json.Unmarshal([]byte(emailRaw), &emails); err != nil {
		return nil, errors.New("notification_emails must be a JSON array of strings")
	}

	notificationEmailLimit := s.config.GetNotificationEmailLimit()
	if len(emails) > notificationEmailLimit {
		return nil, fmt.Errorf("notification_emails must contain at most %d entries", notificationEmailLimit)
	}

	return emails, nil
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
	ctx := r.Context()
	logger := s.logger.WithContext(ctx)

	ticketID := r.PathValue("id")
	if ticketID == "" {
		s.writeAPIError(ctx, logger, w, metrics.EndpointStatus, http.StatusBadRequest, "id parameter must be provided")
		return
	}

	ticketIDParsed, err := uuid.Parse(ticketID)
	if err != nil {
		s.writeAPIError(ctx, logger, w, metrics.EndpointStatus, http.StatusBadRequest, "id must be a valid UUID")
		return
	}

	ctx = logging.WithTicketID(ctx, ticketIDParsed.String())
	logger = logger.WithContext(ctx)

	ticket, err := s.job.Get(ctx, ticketIDParsed)
	if err != nil {
		if errors.Is(err, job.ErrRecordNotFound) {
			s.writeAPIError(ctx, logger, w, metrics.EndpointStatus, http.StatusNotFound, "ticket was not found")
		} else {
			logger.Error("ticket status lookup failed", "component", logging.ComponentDatabase, "operation", logging.OperationTicketLookup, "error", err)
			s.writeAPIError(ctx, logger, w, metrics.EndpointStatus, http.StatusInternalServerError, "failed to retrieve ticket status")
		}
		return
	}

	responseBody, err := json.Marshal(ticket)
	if err != nil {
		logger.Error("ticket status response serialization failed", "component", logging.ComponentHTTP, "operation", logging.OperationResponseSerialization, "error", err)
		s.writeAPIError(ctx, logger, w, metrics.EndpointStatus, http.StatusInternalServerError, "failed to retrieve ticket status")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(responseBody); err != nil {
		logger.Warn("response write failed", "component", logging.ComponentHTTP, "status_code", http.StatusOK, "error", err, "operation", logging.OperationErrorResponseWrite)
		s.metrics.RecordResponseWriteError(ctx, metrics.ResponseObservation{Endpoint: metrics.EndpointStatus})
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
	ctx := r.Context()
	logger := s.logger.WithContext(ctx)

	ticketID := r.PathValue("id")
	if ticketID == "" {
		s.writeAPIError(ctx, logger, w, metrics.EndpointResults, http.StatusBadRequest, "id parameter must be provided")
		return
	}

	ticketIDParsed, err := uuid.Parse(ticketID)
	if err != nil {
		s.writeAPIError(ctx, logger, w, metrics.EndpointResults, http.StatusBadRequest, "id must be a valid UUID")
		return
	}

	ctx = logging.WithTicketID(ctx, ticketIDParsed.String())
	logger = logger.WithContext(ctx)

	ticket, err := s.job.Get(ctx, ticketIDParsed)
	if err != nil {
		if errors.Is(err, job.ErrRecordNotFound) {
			s.writeAPIError(ctx, logger, w, metrics.EndpointResults, http.StatusNotFound, "ticket was not found")
		} else {
			logger.Error("ticket status lookup failed", "component", logging.ComponentDatabase, "operation", logging.OperationTicketLookup, "error", err)
			s.writeAPIError(ctx, logger, w, metrics.EndpointResults, http.StatusInternalServerError, "failed to retrieve processing result")
		}
		return
	}

	if ticket.Status != job.Complete {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	outputPath := s.config.GetOutputPath(s.config.GetBucket(), s.config.GetBucketPath(), ticket.ID.String())

	link, expiration, err := s.storage.GetLink(ctx, outputPath)
	if err != nil {
		logger.Error("failed to generate presigned link", "component", logging.ComponentStorage, "operation", logging.OperationStoragePresign, "error", err)
		s.writeAPIError(ctx, logger, w, metrics.EndpointResults, http.StatusInternalServerError, "failed to retrieve processing result")
		return
	}

	result := resultOutput{
		Link:    link,
		Expires: expiration,
	}

	b, err := json.Marshal(result)
	if err != nil {
		logger.Error("failed to marshal presigned result obj", "component", logging.ComponentHTTP, "operation", logging.OperationResponseSerialization, "error", err)
		s.writeAPIError(ctx, logger, w, metrics.EndpointResults, http.StatusInternalServerError, "failed to retrieve processing result")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(b); err != nil {
		logger.Error("failed to write response", "component", logging.ComponentHTTP, "status_code", http.StatusOK, "error", err, "operation", logging.OperationErrorResponseWrite)
		s.metrics.RecordResponseWriteError(ctx, metrics.ResponseObservation{Endpoint: metrics.EndpointResults})
	}
}

type apiError struct {
	Detail string `json:"detail"`
}

func (s *ServiceV1) writeAPIError(
	ctx context.Context,
	logger logging.Logger,
	w http.ResponseWriter,
	endpoint metrics.EndpointV1,
	statusCode int,
	detail string,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(apiError{Detail: detail}); err != nil {
		s.metrics.RecordResponseWriteError(ctx, metrics.ResponseObservation{Endpoint: endpoint})
		logger.Warn(
			"error response write failed",
			"component", logging.ComponentHTTP,
			"status_code", statusCode,
			"error", err,
			"operation", logging.OperationErrorWriteFailed,
		)
	}
}
