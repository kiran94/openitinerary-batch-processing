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
	"github.com/kiran94/openitinerary-batch-processing/internal/service"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry/metrics"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type ServiceV1 struct {
	config  config.Config
	job     job.TicketRepository
	logger  logging.Logger
	storage storage.Storage
	metrics telemetry.ServiceMetrics
	tracer  trace.Tracer

	submitService service.SubmitService
	resultService service.ResultService
}

type submitRequest struct {
	file                multipart.File
	fileHeader          *multipart.FileHeader
	contentType         string
	fileType            string
	documentContentType config.ContentType
	notificationEmails  []string
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
	span := trace.SpanFromContext(r.Context())

	defer func() {
		if r.MultipartForm == nil {
			return
		}

		err := r.MultipartForm.RemoveAll()
		if err != nil {
			logger.Warn(ctx, "multipart form cleanup failed", "component", logging.ComponentHTTP, "operation", logging.OperationMultipartCleanup, "error", err)
		}
	}()

	request, requestErr := s.parseSubmitRequest(w, r)
	if request.file != nil {
		defer request.file.Close()
	}
	if requestErr != nil {
		s.writeAPIError(ctx, logger, w, metrics.EndpointSubmit, requestErr.statusCode, requestErr.detail, span)
		reason := requestErr.reason
		s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
			Outcome: metrics.SubmissionRejected,
			Reason:  &reason,
		})
		return
	}

	input := service.SubmitInput{
		Document:           request.file,
		SizeBytes:          request.fileHeader.Size,
		ContentType:        request.documentContentType,
		NotificationEmails: request.notificationEmails,
		SubmitStartedAt:    startedAt,
	}

	ticket, err := s.submitService.Submit(ctx, input)
	if err != nil {
		if e, ok := errors.AsType[service.ErrInvalidNotificationEmails](err); ok {
			s.writeAPIError(ctx, logger, w, metrics.EndpointSubmit, http.StatusBadRequest, e.Error(), span)
			return
		}

		s.writeAPIError(ctx, logger, w, metrics.EndpointSubmit, http.StatusInternalServerError, "failed to submit document", span)
		return
	}

	ctx = logging.WithTicketID(ctx, ticket.ID.String())
	logger = logger.WithContext(ctx)

	s.writeSubmitResponse(ctx, logger, w, request, ticket, startedAt)
}

func (s *ServiceV1) parseSubmitRequest(w http.ResponseWriter, r *http.Request) (submitRequest, *submitRequestError) {
	_, span := s.tracer.Start(r.Context(), string(telemetry.TracingSpanParseSubmission))
	defer span.End()

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

	fileType, documentContentType, err := contentTypeConversion(contentType)
	if err != nil {
		return request, &submitRequestError{
			statusCode: http.StatusUnsupportedMediaType,
			detail:     fmt.Sprintf("unsupported file content type: %s", contentType),
			reason:     metrics.SubmissionReasonUnsupportedContentType,
		}
	}

	request.contentType = contentType
	request.fileType = fileType
	request.documentContentType = documentContentType
	request.notificationEmails = emails
	return request, nil
}

func (s *ServiceV1) writeSubmitResponse(
	ctx context.Context,
	logger logging.Logger,
	w http.ResponseWriter,
	request submitRequest,
	ticket *job.Ticket,
	startedAt time.Time,
) {
	ctx, span := s.tracer.Start(ctx, string(telemetry.TracingSpanWriteResponse))
	defer span.End()

	responseBody, err := json.Marshal(ticket)
	inputBytes := request.fileHeader.Size
	if err != nil {
		logger.Error(ctx, "submit response serialization failed", "component", logging.ComponentHTTP, "operation", logging.OperationResponseSerialization, "error", err)
		s.writeAPIError(ctx, logger, w, metrics.EndpointSubmit, http.StatusInternalServerError, "failed to submit document", span)
		s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
			Outcome:     metrics.SubmissionFailed,
			Reason:      new(metrics.SubmissionReasonResponseSerializationFailed),
			ContentType: &request.documentContentType,
			InputBytes:  &inputBytes,
			Enqueued:    true,
		})
		span.RecordError(err)
		return
	}

	s.metrics.RecordSubmission(ctx, metrics.SubmissionObservation{
		Outcome:     metrics.SubmissionAccepted,
		Enqueued:    true,
		InputBytes:  &inputBytes,
		ContentType: &request.documentContentType,
	})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)

	if _, err := w.Write(responseBody); err != nil {
		logger.Warn(ctx,
			"response write failed",
			"component", logging.ComponentHTTP,
			"status_code", http.StatusAccepted,
			"error", err,
			"operation", logging.OperationErrorResponseWrite,
		)
		s.metrics.RecordResponseWriteError(ctx, metrics.ResponseObservation{Endpoint: metrics.EndpointSubmit})
		span.RecordError(err)
		return
	}

	logger.Info(ctx,
		"submit accepted",
		"content_type", request.contentType,
		"duration_ms", time.Since(startedAt).Milliseconds(),
		"component", logging.ComponentHTTP,
		"operation", logging.OperationSubmit,
	)

	span.SetStatus(codes.Ok, "")
}

func contentTypeConversion(contentType string) (string, config.ContentType, error) {
	switch contentType {
	case "application/pdf":
		return "pdf", config.ContentTypePDF, nil
	case "text/html":
		return "html", config.ContentTypeHTML, nil
	case "message/rfc822":
		return "eml", config.ContentTypeEmail, nil
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
	span := trace.SpanFromContext(r.Context())

	ticketID := r.PathValue("id")
	if ticketID == "" {
		s.writeAPIError(ctx, logger, w, metrics.EndpointStatus, http.StatusBadRequest, "id parameter must be provided", span)
		return
	}

	ticketIDParsed, err := uuid.Parse(ticketID)
	if err != nil {
		s.writeAPIError(ctx, logger, w, metrics.EndpointStatus, http.StatusBadRequest, "id must be a valid UUID", span)
		return
	}

	ctx = logging.WithTicketID(ctx, ticketIDParsed.String())
	logger = logger.WithContext(ctx)

	ticket, err := s.job.Get(ctx, ticketIDParsed)
	if err != nil {
		if errors.Is(err, job.ErrRecordNotFound) {
			s.writeAPIError(ctx, logger, w, metrics.EndpointStatus, http.StatusNotFound, "ticket was not found", span)
		} else {
			logger.Error(ctx, "ticket status lookup failed", "component", logging.ComponentDatabase, "operation", logging.OperationTicketLookup, "error", err)
			s.writeAPIError(ctx, logger, w, metrics.EndpointStatus, http.StatusInternalServerError, "failed to retrieve ticket status", span)
		}
		return
	}

	responseBody, err := json.Marshal(ticket)
	if err != nil {
		logger.Error(ctx, "ticket status response serialization failed", "component", logging.ComponentHTTP, "operation", logging.OperationResponseSerialization, "error", err)
		s.writeAPIError(ctx, logger, w, metrics.EndpointStatus, http.StatusInternalServerError, "failed to retrieve ticket status", span)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(responseBody); err != nil {
		logger.Warn(ctx, "response write failed", "component", logging.ComponentHTTP, "status_code", http.StatusOK, "error", err, "operation", logging.OperationErrorResponseWrite)
		s.metrics.RecordResponseWriteError(ctx, metrics.ResponseObservation{Endpoint: metrics.EndpointStatus})
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to write response")
		return
	}

	span.SetStatus(codes.Ok, "")
}

// result returns a temporary download link for a successfully completed ticket.
// @Summary      Get processing result
// @Description  Generate a temporary download link for the result of a successfully completed ticket.
// @Tags         batch
// @Produce      json
// @Param        id  path      string        true  "Ticket UUID"
// @Success      200 {object}  service.ResultOutput  "Temporary result download link"
// @Success      204 "Ticket has not successfully completed"
// @Failure      400 {object}  apiError     "Missing or invalid ticket UUID"
// @Failure      404 {object}  apiError     "Ticket not found"
// @Failure      500 {object}  apiError     "Failed to retrieve the processing result"
// @Router       /api/v1/result/{id} [get]
func (s *ServiceV1) result(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := s.logger.WithContext(ctx)
	span := trace.SpanFromContext(r.Context())

	ticketID := r.PathValue("id")
	if ticketID == "" {
		s.writeAPIError(ctx, logger, w, metrics.EndpointResults, http.StatusBadRequest, "id parameter must be provided", span)
		return
	}

	ticketIDParsed, err := uuid.Parse(ticketID)
	if err != nil {
		s.writeAPIError(ctx, logger, w, metrics.EndpointResults, http.StatusBadRequest, "id must be a valid UUID", span)
		return
	}

	ctx = logging.WithTicketID(ctx, ticketIDParsed.String())
	logger = logger.WithContext(ctx)

	result, err := s.resultService.GetResultLink(ctx, ticketIDParsed)
	if err != nil {
		if errors.Is(err, service.ErrJobNotComplete) {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		if errors.Is(err, job.ErrRecordNotFound) {
			s.writeAPIError(ctx, logger, w, metrics.EndpointResults, http.StatusNotFound, "ticket was not found", span)
			return
		}

		s.writeAPIError(ctx, logger, w, metrics.EndpointResults, http.StatusInternalServerError, "failed to retrieve processing result", span)
		return
	}

	b, err := json.Marshal(result)
	if err != nil {
		logger.Error(ctx, "failed to marshal presigned result obj", "component", logging.ComponentHTTP, "operation", logging.OperationResponseSerialization, "error", err)
		s.writeAPIError(ctx, logger, w, metrics.EndpointResults, http.StatusInternalServerError, "failed to retrieve processing result", span)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(b); err != nil {
		logger.Error(ctx, "failed to write response", "component", logging.ComponentHTTP, "status_code", http.StatusOK, "error", err, "operation", logging.OperationErrorResponseWrite)
		s.metrics.RecordResponseWriteError(ctx, metrics.ResponseObservation{Endpoint: metrics.EndpointResults})
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to write response")
		return
	}

	span.SetStatus(codes.Ok, "")
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
	span trace.Span,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	// User Errors should not set the span status
	// https://opentelemetry.io/docs/specs/semconv/http/http-spans/
	if statusCode >= http.StatusInternalServerError {
		span.SetStatus(codes.Error, detail)
	}

	if err := json.NewEncoder(w).Encode(apiError{Detail: detail}); err != nil {
		s.metrics.RecordResponseWriteError(ctx, metrics.ResponseObservation{Endpoint: endpoint})
		span.SetStatus(codes.Error, "failed to serialise api error to json")
		span.RecordError(err)
		logger.Warn(ctx,
			"error response write failed",
			"component", logging.ComponentHTTP,
			"status_code", statusCode,
			"error", err,
			"operation", logging.OperationErrorWriteFailed,
		)
	}
}
