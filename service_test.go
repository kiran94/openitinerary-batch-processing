package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/service"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	telemetrymetrics "github.com/kiran94/openitinerary-batch-processing/internal/telemetry/metrics"
	"github.com/kiran94/openitinerary-batch-processing/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestServiceSubmit(t *testing.T) {
	tests := []submitSuccessCase{
		{
			name:        "PDF",
			contentType: "application/pdf",
			data:        []byte("my pdf data"),
		},
		{
			name:        "HTML",
			contentType: "text/html",
			data:        []byte("<html><body>test</body></html>"),
		},
		{
			name:        "Email",
			contentType: "message/rfc822",
			data:        []byte("Subject: Test\r\n\r\nEmail body"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testServiceSubmitSuccess(t, test)
		})
	}
}

func TestContentTypeConversion(t *testing.T) {
	tests := []struct {
		name                string
		input               string
		expectedFileType    string
		expectedContentType config.ContentType
		expectsError        bool
	}{
		{name: "PDF", input: "application/pdf", expectedFileType: "pdf", expectedContentType: config.ContentTypePDF},
		{name: "HTML", input: "text/html", expectedFileType: "html", expectedContentType: config.ContentTypeHTML},
		{name: "Email", input: "message/rfc822", expectedFileType: "eml", expectedContentType: config.ContentTypeEmail},
		{name: "Unsupported", input: "image/png", expectsError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fileType, documentContentType, err := contentTypeConversion(test.input)
			if test.expectsError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), test.input)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, test.expectedFileType, fileType)
			assert.Equal(t, test.expectedContentType, documentContentType)
		})
	}
}

func TestParseNotificationEmails(t *testing.T) {
	tests := []struct {
		name          string
		raw           string
		expected      []string
		expectedError string
	}{
		{
			name:     "valid",
			raw:      `["traveller@example.com"]`,
			expected: []string{"traveller@example.com"},
		},
		{
			name:          "invalid JSON",
			raw:           "not-json",
			expectedError: "notification_emails must be a JSON array of strings",
		},
		{
			name:     "multiple",
			raw:      `["one@example.com", "two@example.com"]`,
			expected: []string{"one@example.com", "two@example.com"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", nil)
			request.Form = url.Values{"notification_emails": []string{test.raw}}

			service := ServiceV1{}
			emails, err := service.parseNotificationEmails(request)

			if test.expectedError != "" {
				require.EqualError(t, err, test.expectedError)
				assert.Nil(t, emails)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.expected, emails)
			}
		})
	}
}

func TestServiceSubmitMaximumBytesRequest(t *testing.T) {
	rawBuffer := []byte("large request") // 13 bytes
	maximumAllowedBytes := int64(14)     // 14 bytes

	mockConfig := MockConfig{}
	mockMetrics := new(MockServiceMetrics)
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(maximumAllowedBytes).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockMetrics.
		On(
			"RecordSubmission",
			t.Context(),
			telemetrymetrics.SubmissionObservation{
				Outcome:     telemetrymetrics.SubmissionRejected,
				Reason:      new(telemetrymetrics.SubmissionReasonRequestBodyTooLarge),
				ServiceType: telemetrymetrics.SubmissionServiceTypeHTTP,
			},
		).
		Once()

	writer := httptest.NewRecorder()

	body, contentType := createTestMultipartFormBuffer(t, rawBuffer, "application/pdf")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	service := ServiceV1{config: &mockConfig, logger: &MockLogger{}, metrics: mockMetrics, tracer: testutils.NewMockTracer()}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusRequestEntityTooLarge, "request body is too large")
	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
}

func TestServiceSubmitBadMultipartForm(t *testing.T) {
	mockConfig := MockConfig{}
	mockMetrics := new(MockServiceMetrics)
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockMetrics.
		On(
			"RecordSubmission",
			t.Context(),
			telemetrymetrics.SubmissionObservation{
				Outcome:     telemetrymetrics.SubmissionRejected,
				Reason:      new(telemetrymetrics.SubmissionReasonInvalidMultipartForm),
				ServiceType: telemetrymetrics.SubmissionServiceTypeHTTP,
			},
		).
		Once()

	writer := httptest.NewRecorder()
	body, _ := createTestMultipartFormBuffer(t, []byte("data"), "application/pdf")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)

	// Skip adding the content type to make this an invalid multipart form
	// request.Header.Set("Content-Type", contentType)

	service := ServiceV1{config: &mockConfig, logger: &MockLogger{}, metrics: mockMetrics, tracer: testutils.NewMockTracer()}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusBadRequest, "invalid multipart form")
	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
}

func TestServiceSubmitValidMultipartButNotFile(t *testing.T) {
	mockConfig := MockConfig{}
	mockMetrics := new(MockServiceMetrics)
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockMetrics.
		On(
			"RecordSubmission",
			t.Context(),
			telemetrymetrics.SubmissionObservation{
				Outcome:     telemetrymetrics.SubmissionRejected,
				Reason:      new(telemetrymetrics.SubmissionReasonFileNotProvided),
				ServiceType: telemetrymetrics.SubmissionServiceTypeHTTP,
			},
		).
		Once()

	writer := httptest.NewRecorder()
	body, contentType := createTestMultipartFormWithoutFileBuffer(t)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	service := ServiceV1{config: &mockConfig, logger: &MockLogger{}, metrics: mockMetrics, tracer: testutils.NewMockTracer()}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusBadRequest, "file must be provided")

	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
}

func TestServiceSubmitMissingInnerContentType(t *testing.T) {
	mockConfig := MockConfig{}
	mockMetrics := new(MockServiceMetrics)
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockMetrics.
		On(
			"RecordSubmission",
			t.Context(),
			telemetrymetrics.SubmissionObservation{
				Outcome:     telemetrymetrics.SubmissionRejected,
				Reason:      new(telemetrymetrics.SubmissionReasonContentTypeNotProvided),
				ServiceType: telemetrymetrics.SubmissionServiceTypeHTTP,
			},
		).
		Once()

	writer := httptest.NewRecorder()
	body, contentType := createTestMultipartFormWithoutContentType(t)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	service := ServiceV1{config: &mockConfig, logger: &MockLogger{}, metrics: mockMetrics, tracer: testutils.NewMockTracer()}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusBadRequest, "file content type must be provided")
	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
}

func TestServiceSubmitInvalidMimeContentType(t *testing.T) {

	mockConfig := MockConfig{}
	mockMetrics := new(MockServiceMetrics)
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockMetrics.
		On(
			"RecordSubmission",
			t.Context(),
			telemetrymetrics.SubmissionObservation{
				Outcome:     telemetrymetrics.SubmissionRejected,
				Reason:      new(telemetrymetrics.SubmissionReasonInvalidContentType),
				ServiceType: telemetrymetrics.SubmissionServiceTypeHTTP,
			},
		).
		Once()

	writer := httptest.NewRecorder()
	body, contentType := createTestMultipartFormBuffer(t, []byte("data"), "NOT A MIME TYPE")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	service := ServiceV1{config: &mockConfig, logger: &MockLogger{}, metrics: mockMetrics, tracer: testutils.NewMockTracer()}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusUnsupportedMediaType, "file content type is invalid")
	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
}

func TestServiceSubmitUnsupportedFileFormat(t *testing.T) {
	mockConfig := new(MockConfig)
	mockMetrics := new(MockServiceMetrics)
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockMetrics.
		On(
			"RecordSubmission",
			t.Context(),
			telemetrymetrics.SubmissionObservation{
				Outcome:     telemetrymetrics.SubmissionRejected,
				Reason:      new(telemetrymetrics.SubmissionReasonUnsupportedContentType),
				ServiceType: telemetrymetrics.SubmissionServiceTypeHTTP,
			},
		).
		Once()

	body, contentType := createTestMultipartFormBuffer(t, []byte("image data"), "image/png")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	writer := httptest.NewRecorder()
	service := ServiceV1{config: mockConfig, logger: &MockLogger{}, metrics: mockMetrics, tracer: testutils.NewMockTracer()}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusUnsupportedMediaType, "unsupported file content type: image/png")
	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
}

func TestServiceSubmitFailure(t *testing.T) {
	mockConfig := new(MockConfig)
	mockMetrics := new(MockServiceMetrics)
	mockSubmitService := new(MockSubmitService)
	document := []byte("my pdf data")

	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockSubmitService.
		On("Submit", t.Context(), mock.AnythingOfType("service.SubmitInput")).
		Run(assertSubmitInput(t, document, config.ContentTypePDF, testNotificationEmails)).
		Return(nil, errors.New("submission failed")).
		Once()

	body, contentType := createTestMultipartFormBuffer(t, document, "application/pdf")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	writer := httptest.NewRecorder()
	httpService := ServiceV1{
		config:        mockConfig,
		logger:        &MockLogger{},
		metrics:       mockMetrics,
		tracer:        testutils.NewMockTracer(),
		submitService: mockSubmitService,
	}
	httpService.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusInternalServerError, "failed to submit document")

	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
	mockSubmitService.AssertExpectations(t)
}

func TestServiceSubmitResponseWriteError(t *testing.T) {
	mockConfig := new(MockConfig)
	mockMetrics := new(MockServiceMetrics)
	mockSubmitService := new(MockSubmitService)
	document := []byte("my pdf data")
	expectedContentType := config.ContentTypePDF
	expectedInputBytes := int64(len(document))
	expectedTicket := job.NewTicket()

	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockSubmitService.
		On("Submit", t.Context(), mock.AnythingOfType("service.SubmitInput")).
		Run(assertSubmitInput(t, document, expectedContentType, testNotificationEmails)).
		Return(expectedTicket, nil).
		Once()
	mockMetrics.
		On(
			"RecordSubmission",
			contextHasTicketIDValue(expectedTicket.ID),
			telemetrymetrics.SubmissionObservation{
				Outcome:     telemetrymetrics.SubmissionAccepted,
				ContentType: &expectedContentType,
				InputBytes:  &expectedInputBytes,
				Enqueued:    true,
				ServiceType: telemetrymetrics.SubmissionServiceTypeHTTP,
			},
		).
		Once()
	mockMetrics.
		On(
			"RecordResponseWriteError",
			contextHasTicketIDValue(expectedTicket.ID),
			telemetrymetrics.ResponseObservation{Endpoint: config.EndpointSubmit},
		).
		Once()

	httpService := ServiceV1{
		config:        mockConfig,
		logger:        &MockLogger{},
		metrics:       mockMetrics,
		tracer:        testutils.NewMockTracer(),
		submitService: mockSubmitService,
	}

	body, contentType := createTestMultipartFormBuffer(t, document, "application/pdf")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)
	writer := &responseWriteErrorRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		err:              errors.New("response write failed"),
	}

	httpService.submit(writer, request)

	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
	mockSubmitService.AssertExpectations(t)
}

func TestServiceSubmitTooManyNotificationEmails(t *testing.T) {
	mockConfig := new(MockConfig)
	mockMetrics := new(MockServiceMetrics)
	mockSubmitService := new(MockSubmitService)
	document := []byte("my pdf data")
	invalidEmailsError := service.ErrInvalidNotificationEmails{}

	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockSubmitService.
		On("Submit", t.Context(), mock.AnythingOfType("service.SubmitInput")).
		Run(assertSubmitInput(t, document, config.ContentTypePDF, testNotificationEmails)).
		Return(nil, invalidEmailsError).
		Once()

	body, contentType := createTestMultipartFormBuffer(t, document, "application/pdf")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	writer := httptest.NewRecorder()
	httpService := ServiceV1{
		config:        mockConfig,
		logger:        &MockLogger{},
		metrics:       mockMetrics,
		tracer:        testutils.NewMockTracer(),
		submitService: mockSubmitService,
	}
	httpService.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusBadRequest, invalidEmailsError.Error())

	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
	mockSubmitService.AssertExpectations(t)
}

func TestServiceSubmitInvalidNotificationEmails(t *testing.T) {
	mockConfig := new(MockConfig)
	mockMetrics := new(MockServiceMetrics)
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockMetrics.
		On(
			"RecordSubmission",
			t.Context(),
			telemetrymetrics.SubmissionObservation{
				Outcome:     telemetrymetrics.SubmissionRejected,
				Reason:      new(telemetrymetrics.SubmissionReasonInvalidNotificationEmails),
				ServiceType: telemetrymetrics.SubmissionServiceTypeHTTP,
			},
		).
		Once()

	body, contentType := createTestMultipartFormBufferWithNotificationEmails(
		t,
		[]byte("my pdf data"),
		"application/pdf",
		"not-json",
	)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	writer := httptest.NewRecorder()
	service := ServiceV1{config: mockConfig, logger: &MockLogger{}, metrics: mockMetrics, tracer: testutils.NewMockTracer()}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusBadRequest, "notification_emails must be a JSON array of strings")
	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
}

func TestServiceStatusInvalidTicket(t *testing.T) {
	tests := []struct {
		name           string
		inputTicket    string
		expectedDetail string
	}{
		{name: "missing", inputTicket: "", expectedDetail: "id parameter must be provided"},
		{name: "invalid", inputTicket: "123", expectedDetail: "id must be a valid UUID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statusService := new(MockStatusService)
			writer := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
			request.SetPathValue("id", tt.inputTicket)

			httpService := ServiceV1{
				logger:        &MockLogger{},
				tracer:        testutils.NewMockTracer(),
				statusService: statusService,
			}
			httpService.status(writer, request)

			response := writer.Result()
			defer response.Body.Close()

			assertAPIError(t, response, http.StatusBadRequest, tt.expectedDetail)
			statusService.AssertNotCalled(t, "GetStatus", mock.Anything, mock.Anything)
		})
	}
}

func TestServiceStatusTicketLookupFailure(t *testing.T) {
	inputTicket := uuid.New()
	tests := []struct {
		name           string
		getJobError    error
		expectedStatus int
		expectedDetail string
	}{
		{
			name:           "ticket_not_found",
			getJobError:    job.ErrRecordNotFound,
			expectedStatus: http.StatusNotFound,
			expectedDetail: "ticket was not found",
		},
		{
			name:           "repository_failure",
			getJobError:    errors.New("failed to get ticket"),
			expectedStatus: http.StatusInternalServerError,
			expectedDetail: "failed to retrieve ticket status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statusService := new(MockStatusService)
			statusService.
				On("GetStatus", contextHasTicketIDValue(inputTicket), inputTicket).
				Return(nil, tt.getJobError).
				Once()

			writer := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
			request.SetPathValue("id", inputTicket.String())

			httpService := ServiceV1{
				logger:        &MockLogger{},
				tracer:        testutils.NewMockTracer(),
				statusService: statusService,
			}
			httpService.status(writer, request)

			response := writer.Result()
			defer response.Body.Close()

			assertAPIError(t, response, tt.expectedStatus, tt.expectedDetail)
			statusService.AssertExpectations(t)
		})
	}
}

func TestServiceStatusSuccess(t *testing.T) {
	inputTicket := uuid.New()
	expectedTicket := job.NewTicket()
	expectedTicket.ID = inputTicket
	statusService := new(MockStatusService)
	statusService.
		On("GetStatus", contextHasTicketIDValue(inputTicket), inputTicket).
		Return(expectedTicket, nil).
		Once()

	writer := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
	request.SetPathValue("id", inputTicket.String())

	httpService := ServiceV1{
		logger:        &MockLogger{},
		tracer:        testutils.NewMockTracer(),
		statusService: statusService,
	}
	httpService.status(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, "application/json", response.Header.Get("Content-Type"))

	var ticket job.Ticket
	require.NoError(t, json.NewDecoder(response.Body).Decode(&ticket))
	assert.Equal(t, expectedTicket.ID, ticket.ID)
	assert.Equal(t, expectedTicket.Status, ticket.Status)

	statusService.AssertExpectations(t)
}

func TestServiceStatusResponseWriteError(t *testing.T) {
	inputTicket := uuid.New()
	ticket := job.NewTicket()
	ticket.ID = inputTicket

	statusService := new(MockStatusService)
	statusService.
		On("GetStatus", contextHasTicketIDValue(inputTicket), inputTicket).
		Return(ticket, nil).
		Once()

	serviceMetrics := new(MockServiceMetrics)
	serviceMetrics.
		On(
			"RecordResponseWriteError",
			contextHasTicketIDValue(inputTicket),
			telemetrymetrics.ResponseObservation{Endpoint: config.EndpointStatus},
		).
		Once()

	writer := &responseWriteErrorRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		err:              errors.New("response write failed"),
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
	request.SetPathValue("id", inputTicket.String())

	httpService := ServiceV1{
		logger:        &MockLogger{},
		metrics:       serviceMetrics,
		tracer:        testutils.NewMockTracer(),
		statusService: statusService,
	}
	httpService.status(writer, request)

	serviceMetrics.AssertExpectations(t)
	statusService.AssertExpectations(t)
}

func TestServiceResultInvalidTicket(t *testing.T) {

	tests := []struct {
		name           string
		inputTicket    string
		expectedDetail string
	}{
		{name: "missing", inputTicket: "", expectedDetail: "id parameter must be provided"},
		{name: "invalid", inputTicket: "123", expectedDetail: "id must be a valid UUID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultService := new(MockResultService)

			w := httptest.NewRecorder()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
			r.SetPathValue("id", tt.inputTicket)

			httpService := ServiceV1{
				logger:        new(MockLogger),
				tracer:        testutils.NewMockTracer(),
				resultService: resultService,
			}
			httpService.result(w, r)

			result := w.Result()
			defer result.Body.Close()

			assertAPIError(t, result, http.StatusBadRequest, tt.expectedDetail)
			resultService.AssertNotCalled(t, "GetResultLink", mock.Anything, mock.Anything)
		})
	}
}

func TestServiceResultTicketNotFound(t *testing.T) {
	inputTicket := uuid.New()
	tests := []struct {
		name           string
		getJobError    error
		expectedStatus int
		expectedDetail string
	}{
		{
			name:           "ticket_not_found",
			getJobError:    job.ErrRecordNotFound,
			expectedStatus: http.StatusNotFound,
			expectedDetail: "ticket was not found",
		},
		{
			name:           "failed_ticket_get",
			getJobError:    errors.New("failed to get ticket"),
			expectedStatus: http.StatusInternalServerError,
			expectedDetail: "failed to retrieve processing result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultService := new(MockResultService)
			resultService.
				On("GetResultLink", contextHasTicketIDValue(inputTicket), inputTicket).
				Return(nil, tt.getJobError).
				Once()

			w := httptest.NewRecorder()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
			r.SetPathValue("id", inputTicket.String())

			httpService := ServiceV1{
				logger:        new(MockLogger),
				tracer:        testutils.NewMockTracer(),
				resultService: resultService,
			}
			httpService.result(w, r)

			result := w.Result()
			defer result.Body.Close()

			assertAPIError(t, result, tt.expectedStatus, tt.expectedDetail)

			resultService.AssertExpectations(t)
		})
	}
}

func TestServiceResultTicketNotComplete(t *testing.T) {
	inputTicket := uuid.New()
	resultService := new(MockResultService)
	resultService.
		On("GetResultLink", contextHasTicketIDValue(inputTicket), inputTicket).
		Return(nil, service.ErrJobNotComplete).
		Once()

	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
	r.SetPathValue("id", inputTicket.String())

	httpService := ServiceV1{
		logger:        new(MockLogger),
		tracer:        testutils.NewMockTracer(),
		resultService: resultService,
	}
	httpService.result(w, r)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusNoContent, result.StatusCode)
	assert.Empty(t, result.Header.Get("Content-Type"))
	responseBody, err := io.ReadAll(result.Body)
	require.NoError(t, err)
	assert.Empty(t, responseBody)

	resultService.AssertExpectations(t)
}

func TestServiceResultSuccess(t *testing.T) {
	inputTicket := uuid.New()
	expectedPresignedLink := "http://s3_presigned_link"
	expectedExpiration := time.Now().Add(time.Second * 60)
	expectedResult := &service.ResultOutput{
		Link:    expectedPresignedLink,
		Expires: expectedExpiration,
	}
	resultService := new(MockResultService)
	resultService.
		On("GetResultLink", contextHasTicketIDValue(inputTicket), inputTicket).
		Return(expectedResult, nil).
		Once()

	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
	r.SetPathValue("id", inputTicket.String())

	httpService := ServiceV1{
		logger:        new(MockLogger),
		tracer:        testutils.NewMockTracer(),
		resultService: resultService,
	}
	httpService.result(w, r)

	result := w.Result()
	defer result.Body.Close()

	require.Equal(t, http.StatusOK, result.StatusCode)

	b, err := io.ReadAll(result.Body)
	require.NoError(t, err)
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))
	assert.Contains(t, string(b), `"link":"`+expectedPresignedLink+`"`)
	assert.Contains(t, string(b), `"expires":"`+expectedExpiration.Format(time.RFC3339Nano)+`"`)

	var ro service.ResultOutput
	require.NoError(t, json.Unmarshal(b, &ro))

	assert.Equal(t, expectedPresignedLink, ro.Link)
	assert.Equal(t, expectedExpiration.UnixMicro(), ro.Expires.UnixMicro())

	resultService.AssertExpectations(t)
}

func TestServiceResultResponseWriteError(t *testing.T) {
	inputTicket := uuid.New()
	resultService := new(MockResultService)
	resultService.
		On("GetResultLink", contextHasTicketIDValue(inputTicket), inputTicket).
		Return(
			&service.ResultOutput{Link: "http://s3-presigned-link", Expires: time.Now()},
			nil,
		).
		Once()

	serviceMetrics := new(MockServiceMetrics)
	serviceMetrics.
		On(
			"RecordResponseWriteError",
			contextHasTicketIDValue(inputTicket),
			telemetrymetrics.ResponseObservation{Endpoint: config.EndpointResults},
		).
		Once()

	writer := &responseWriteErrorRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		err:              errors.New("response write failed"),
	}
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
	request.SetPathValue("id", inputTicket.String())

	httpService := ServiceV1{
		logger:        &MockLogger{},
		metrics:       serviceMetrics,
		tracer:        testutils.NewMockTracer(),
		resultService: resultService,
	}
	httpService.result(writer, request)

	serviceMetrics.AssertExpectations(t)
	resultService.AssertExpectations(t)
}

func TestWriteAPIErrorRecordsResponseWriteError(t *testing.T) {
	tests := []struct {
		name     string
		endpoint config.EndpointV1
	}{
		{name: "submit", endpoint: config.EndpointSubmit},
		{name: "status", endpoint: config.EndpointStatus},
		{name: "results", endpoint: config.EndpointResults},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serviceMetrics := new(MockServiceMetrics)
			serviceMetrics.
				On(
					"RecordResponseWriteError",
					t.Context(),
					telemetrymetrics.ResponseObservation{Endpoint: tt.endpoint},
				).
				Once()

			responseRecorder := httptest.NewRecorder()
			writer := &responseWriteErrorRecorder{
				ResponseRecorder: responseRecorder,
				err:              errors.New("response write failed"),
			}
			service := ServiceV1{logger: new(MockLogger), metrics: serviceMetrics, tracer: testutils.NewMockTracer()}
			_, span := testutils.NewMockTracer().Start(t.Context(), "write-api-error-test")
			defer span.End()

			service.writeAPIError(
				t.Context(),
				service.logger,
				writer,
				tt.endpoint,
				http.StatusBadRequest,
				"bad request",
				errors.New("bad request"),
				span,
			)

			assert.Equal(t, http.StatusBadRequest, responseRecorder.Code)
			assert.Equal(t, "application/json", responseRecorder.Header().Get("Content-Type"))

			serviceMetrics.AssertExpectations(t)
		})
	}
}

// HELPERS

type submitSuccessCase struct {
	name        string
	contentType string
	data        []byte
}

var testNotificationEmails = []string{
	"traveller@example.com",
	"companion@example.com",
}

func assertAPIError(t *testing.T, response *http.Response, expectedStatus int, expectedDetail string) {
	t.Helper()

	assert.Equal(t, expectedStatus, response.StatusCode)
	assert.Equal(t, "application/json", response.Header.Get("Content-Type"))

	var responseError apiError
	require.NoError(t, json.NewDecoder(response.Body).Decode(&responseError))
	assert.Equal(t, apiError{Detail: expectedDetail}, responseError)
}

func testServiceSubmitSuccess(t *testing.T, test submitSuccessCase) {
	t.Helper()

	mockConfig := new(MockConfig)
	mockMetrics := new(MockServiceMetrics)
	mockSubmitService := new(MockSubmitService)
	expectedContentType := config.ContentType(test.contentType)
	expectedInputBytes := int64(len(test.data))
	expectedTicket := job.NewTicket()
	expectedTicket.DocumentPath = "s3://document/path"
	expectedTicket.ContentType = test.contentType
	expectedTicket.NotificationEmails = testNotificationEmails

	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockSubmitService.
		On("Submit", t.Context(), mock.AnythingOfType("service.SubmitInput")).
		Run(assertSubmitInput(t, test.data, expectedContentType, testNotificationEmails)).
		Return(expectedTicket, nil).
		Once()
	mockMetrics.
		On(
			"RecordSubmission",
			contextHasTicketIDValue(expectedTicket.ID),
			telemetrymetrics.SubmissionObservation{
				Outcome:     telemetrymetrics.SubmissionAccepted,
				ContentType: &expectedContentType,
				InputBytes:  &expectedInputBytes,
				Enqueued:    true,
				ServiceType: telemetrymetrics.SubmissionServiceTypeHTTP,
			},
		).
		Once()

	httpService := ServiceV1{
		config:        mockConfig,
		logger:        &MockLogger{},
		metrics:       mockMetrics,
		tracer:        testutils.NewMockTracer(),
		submitService: mockSubmitService,
	}

	body, requestContentType := createTestMultipartFormBuffer(t, test.data, test.contentType)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", requestContentType)

	writer := httptest.NewRecorder()
	httpService.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assert.Equal(t, http.StatusAccepted, response.StatusCode)
	assert.Equal(t, "application/json", response.Header.Get("Content-Type"))

	var ticket job.Ticket
	require.NoError(t, json.NewDecoder(response.Body).Decode(&ticket))

	assert.NotZero(t, ticket.ID)
	assert.NotZero(t, ticket.CreatedAt)
	assert.NotZero(t, ticket.UpdatedAt)
	assert.Equal(t, expectedTicket.ID, ticket.ID)
	assert.Equal(t, expectedTicket.DocumentPath, ticket.DocumentPath)
	assert.Equal(t, test.contentType, ticket.ContentType)
	assert.Equal(t, testNotificationEmails, ticket.NotificationEmails)
	assert.Equal(t, job.Queued, ticket.Status)

	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
	mockSubmitService.AssertExpectations(t)
}

func assertSubmitInput(t *testing.T, document []byte, contentType config.ContentType, notificationEmails []string) func(mock.Arguments) {
	t.Helper()

	return func(args mock.Arguments) {
		input := args.Get(1).(service.SubmitInput)
		actualDocument, err := io.ReadAll(input.Document)
		require.NoError(t, err)

		assert.Equal(t, document, actualDocument)
		require.NotNil(t, input.SizeBytes)
		assert.Equal(t, int64(len(document)), *input.SizeBytes)
		assert.Equal(t, contentType, input.ContentType)
		assert.Equal(t, notificationEmails, input.NotificationEmails)
		assert.False(t, input.SubmitStartedAt.IsZero())
		assert.Equal(t, time.UTC, input.SubmitStartedAt.Location())
		assert.Equal(t, telemetrymetrics.SubmissionServiceTypeHTTP, string(input.ServiceType))
	}
}

func createTestMultipartFormBuffer(t *testing.T, data []byte, contentType string) (*bytes.Buffer, string) {
	t.Helper()

	notificationEmails, err := json.Marshal(testNotificationEmails)
	require.NoError(t, err)

	return createTestMultipartFormBufferWithNotificationEmails(t, data, contentType, string(notificationEmails))
}

func createTestMultipartFormBufferWithNotificationEmails(
	t *testing.T,
	data []byte,
	contentType string,
	notificationEmails string,
) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)
	require.NoError(t, multipartWriter.WriteField("notification_emails", notificationEmails))

	headers := make(textproto.MIMEHeader)
	headers.Set(
		"Content-Disposition",
		`form-data; name="file"; filename="example.pdf"`,
	)
	headers.Set("Content-Type", contentType)

	file, err := multipartWriter.CreatePart(headers)
	require.NoError(t, err)

	_, err = file.Write(data)
	require.NoError(t, err)
	require.NoError(t, multipartWriter.Close())

	return &body, multipartWriter.FormDataContentType()
}

func createTestMultipartFormWithoutFileBuffer(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)

	require.NoError(
		t,
		multipartWriter.WriteField("description", "no file supplied"),
	)
	require.NoError(t, multipartWriter.Close())

	return &body, multipartWriter.FormDataContentType()
}

func createTestMultipartFormWithoutContentType(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)

	headers := make(textproto.MIMEHeader)
	headers.Set(
		"Content-Disposition",
		`form-data; name="file"; filename="example.pdf"`,
	)
	// Skip to omit the content type of the inner file.
	// headers.Set("Content-Type", "application/pdf")

	file, err := multipartWriter.CreatePart(headers)
	require.NoError(t, err)

	_, err = file.Write([]byte("Hello"))
	require.NoError(t, err)
	require.NoError(t, multipartWriter.Close())

	return &body, multipartWriter.FormDataContentType()
}

// MOCKS

// Compile Time Interface Assertions
// Ensure that each of the mocks implement the interfaces they target.
var (
	_ config.Config            = (*MockConfig)(nil)
	_ job.TicketRepository     = (*MockTicketRepository)(nil)
	_ service.ResultService    = (*MockResultService)(nil)
	_ service.StatusService    = (*MockStatusService)(nil)
	_ service.SubmitService    = (*MockSubmitService)(nil)
	_ storage.Storage          = (*MockStorage)(nil)
	_ telemetry.ServiceMetrics = (*MockServiceMetrics)(nil)
)

type MockConfig struct {
	mock.Mock
}

func (m *MockConfig) GetEnvironment() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetPort() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetLoggingLevel() slog.Level {
	return m.Called().Get(0).(slog.Level)
}

func (m *MockConfig) GetDatabaseConnection() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetServerReadTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *MockConfig) GetServerWriteTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *MockConfig) GetServerIdleTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *MockConfig) GetGrpcServerIdleTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *MockConfig) GetGrpcServerMaxConnectionAge() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *MockConfig) GetGRPCUploadTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *MockConfig) GetMaximumAllowedRequestSizeMb() int64 {
	return m.Called().Get(0).(int64)
}

func (m *MockConfig) GetMaximumBufferMultipartForm() int64 {
	return m.Called().Get(0).(int64)
}

func (m *MockConfig) GetMaximumAdapterResponseSizeBytes() int64 {
	return m.Called().Get(0).(int64)
}

func (m *MockConfig) GetBucket() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetBucketPath() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetS3UploadConcurrency() int {
	return m.Called().Int(0)
}

func (m *MockConfig) GetS3PresignedExpirationSeconds() int {
	return m.Called().Int(0)
}

func (m *MockConfig) GetDocumentPath(bucket, bucketPath, jobID, fileType string) string {
	return m.Called(bucket, bucketPath, jobID, fileType).String(0)
}

func (m *MockConfig) GetOutputPath(bucket, bucketPath, jobID string) string {
	return m.Called(bucket, bucketPath, jobID).String(0)
}

func (m *MockConfig) GetQueueURL() string {
	return m.Called().String()
}

func (m *MockConfig) GetQueuePollingTime() int32 {
	return m.Called().Get(0).(int32)
}

func (m *MockConfig) GetSESConfigurationSetName() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetSESAddressFrom() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetNotificationEmailLimit() int {
	return m.Called().Int(0)
}

func (m *MockConfig) GetWorkerTimeoutSeconds() int {
	return m.Called().Int(0)
}

func (m *MockConfig) GetOtelEnabled() bool {
	return m.Called().Bool(0)
}

func (m *MockConfig) GetOtelServiceName() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetOtelWorkerName() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetOtelGrpcName() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetOtelServiceVersion() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetServiceReadinessPollingInterval() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *MockConfig) GetServiceReadinessPollingTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

type MockTicketRepository struct {
	mock.Mock
}

type MockSubmitService struct {
	mock.Mock
}

type MockResultService struct {
	mock.Mock
}

type MockStatusService struct {
	mock.Mock
}

func (m *MockStatusService) GetStatus(ctx context.Context, ticketID uuid.UUID) (*job.Ticket, error) {
	args := m.Called(ctx, ticketID)

	var ticket *job.Ticket
	if value := args.Get(0); value != nil {
		ticket = value.(*job.Ticket)
	}

	return ticket, args.Error(1)
}

func (m *MockResultService) GetResultLink(ctx context.Context, ticketID uuid.UUID) (*service.ResultOutput, error) {
	args := m.Called(ctx, ticketID)

	var result *service.ResultOutput
	if value := args.Get(0); value != nil {
		result = value.(*service.ResultOutput)
	}

	return result, args.Error(1)
}

func (m *MockSubmitService) Submit(ctx context.Context, input service.SubmitInput) (*job.Ticket, error) {
	args := m.Called(ctx, input)

	var ticket *job.Ticket
	if value := args.Get(0); value != nil {
		ticket = value.(*job.Ticket)
	}

	return ticket, args.Error(1)
}

func (m *MockTicketRepository) Create(ctx context.Context, ticket *job.Ticket) error {
	return m.Called(ctx, ticket).Error(0)
}

func (m *MockTicketRepository) Get(ctx context.Context, id uuid.UUID) (*job.Ticket, error) {
	args := m.Called(ctx, id)

	var ticket *job.Ticket
	if value := args.Get(0); value != nil {
		ticket = value.(*job.Ticket)
	}

	return ticket, args.Error(1)
}

func (m *MockTicketRepository) Update(ctx context.Context, ticket *job.Ticket) error {
	return m.Called(ctx, ticket).Error(0)
}

func (m *MockTicketRepository) MarkFailed(
	ctx context.Context,
	id uuid.UUID,
	reason job.TicketStatusReason,
	detail string,
) (bool, error) {
	args := m.Called(ctx, id, reason, detail)
	return args.Bool(0), args.Error(1)
}

type MockStorage struct {
	mock.Mock
}

type MockServiceMetrics struct {
	mock.Mock
}

type responseWriteErrorRecorder struct {
	*httptest.ResponseRecorder
	err error
}

func (r *responseWriteErrorRecorder) Write([]byte) (int, error) {
	return 0, r.err
}

func (m *MockServiceMetrics) RecordSubmission(ctx context.Context, observation telemetrymetrics.SubmissionObservation) {
	m.Called(ctx, observation)
}

func (m *MockServiceMetrics) RecordStorageUpload(ctx context.Context, observation telemetrymetrics.StorageUploadObservation) {
	m.Called(ctx, observation)
}

func (m *MockServiceMetrics) RecordResponseWriteError(ctx context.Context, observation telemetrymetrics.ResponseObservation) {
	m.Called(ctx, observation)
}

func (m *MockServiceMetrics) RecordJobTerminal(ctx context.Context, observation telemetrymetrics.TerminalObservation) {
	m.Called(ctx, observation)
}

func (m *MockServiceMetrics) RecordJobEndToEnd(ctx context.Context, observation telemetrymetrics.JobEndObservation) {
	m.Called(ctx, observation)
}

func contextHasTicketIDValue(ticketID uuid.UUID) any {
	return mock.MatchedBy(func(ctx context.Context) bool {
		value, ok := logging.GetTicketID(ctx)
		return ok && value == ticketID.String()
	})
}

func (m *MockStorage) Write(ctx context.Context, reader io.Reader, output string) error {
	return m.Called(ctx, reader, output).Error(0)
}

func (m *MockStorage) Read(ctx context.Context, input string) (io.ReadCloser, error) {
	args := m.Called(ctx, input)

	if reader := args.Get(0); reader != nil {
		return reader.(io.ReadCloser), args.Error(1)
	}

	return nil, args.Error(1)
}

func (m *MockStorage) GetLink(ctx context.Context, input string) (string, time.Time, error) {
	args := m.Called(ctx, input)
	return args.String(0), args.Get(1).(time.Time), args.Error(2)
}

type MockLogger struct{}

func (m *MockLogger) Debug(context.Context, string, ...any) {}

func (m *MockLogger) Info(context.Context, string, ...any) {}

func (m *MockLogger) Warn(context.Context, string, ...any) {}

func (m *MockLogger) Error(context.Context, string, ...any) {}

func (m *MockLogger) WithContext(context.Context) logging.Logger { return m }
