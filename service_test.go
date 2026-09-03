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
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	telemetrymetrics "github.com/kiran94/openitinerary-batch-processing/internal/telemetry/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestServiceSubmit(t *testing.T) {
	tests := []submitSuccessCase{
		{
			name:        "PDF",
			contentType: "application/pdf",
			fileType:    "pdf",
			data:        []byte("my pdf data"),
		},
		{
			name:        "HTML",
			contentType: "text/html",
			fileType:    "html",
			data:        []byte("<html><body>test</body></html>"),
		},
		{
			name:        "Email",
			contentType: "message/rfc822",
			fileType:    "eml",
			data:        []byte("Subject: Test\r\n\r\nEmail body"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			testServiceSubmitSuccess(t, test)
		})
	}
}

func TestServiceSubmitMaximumBytesRequest(t *testing.T) {
	rawBuffer := []byte("large request") // 13 bytes
	maximumAllowedBytes := int64(14)     // 14 bytes

	mockConfig := MockConfig{}
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(maximumAllowedBytes).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()

	writer := httptest.NewRecorder()

	body, contentType := createTestMultipartFormBuffer(t, rawBuffer, "application/pdf")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	service := ServiceV1{config: &mockConfig, logger: &MockLogger{}}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusRequestEntityTooLarge, "request body is too large")
	mockConfig.AssertExpectations(t)
}

func TestServiceSubmitBadMultipartForm(t *testing.T) {
	mockConfig := MockConfig{}
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()

	writer := httptest.NewRecorder()
	body, _ := createTestMultipartFormBuffer(t, []byte("data"), "application/pdf")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)

	// Skip adding the content type to make this an invalid multipart form
	// request.Header.Set("Content-Type", contentType)

	service := ServiceV1{config: &mockConfig, logger: &MockLogger{}}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusBadRequest, "invalid multipart form")
	mockConfig.AssertExpectations(t)
}

func TestServiceSubmitValidMultipartButNotFile(t *testing.T) {
	mockConfig := MockConfig{}
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()

	writer := httptest.NewRecorder()
	body, contentType := createTestMultipartFormWithoutFileBuffer(t)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	service := ServiceV1{config: &mockConfig, logger: &MockLogger{}}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusBadRequest, "file must be provided")

	mockConfig.AssertExpectations(t)
}

func TestServiceSubmitMissingInnerContentType(t *testing.T) {
	mockConfig := MockConfig{}
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()

	writer := httptest.NewRecorder()
	body, contentType := createTestMultipartFormWithoutContentType(t)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	service := ServiceV1{config: &mockConfig, logger: &MockLogger{}}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusBadRequest, "file content type must be provided")
	mockConfig.AssertExpectations(t)
}

func TestServiceSubmitInvalidMimeContentType(t *testing.T) {

	mockConfig := MockConfig{}
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()

	writer := httptest.NewRecorder()
	body, contentType := createTestMultipartFormBuffer(t, []byte("data"), "NOT A MIME TYPE")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	service := ServiceV1{config: &mockConfig, logger: &MockLogger{}}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusUnsupportedMediaType, "file content type is invalid")
	mockConfig.AssertExpectations(t)
}

func TestServiceSubmitUnsupportedFileFormat(t *testing.T) {
	mockConfig := new(MockConfig)
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockConfig.On("GetNotificationEmailLimit").Return(5).Once()

	body, contentType := createTestMultipartFormBuffer(t, []byte("image data"), "image/png")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	writer := httptest.NewRecorder()
	service := ServiceV1{config: mockConfig, logger: &MockLogger{}}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusUnsupportedMediaType, "unsupported file content type: image/png")
	mockConfig.AssertExpectations(t)
}

func TestServiceSubmitStorageWriteError(t *testing.T) {
	mockConfig := new(MockConfig)
	mockMetrics := new(MockServiceMetrics)
	mockStorage := new(MockStorage)

	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockConfig.On("GetNotificationEmailLimit").Return(5).Once()
	mockConfig.On("GetBucket").Return("bucket").Once()
	mockConfig.On("GetBucketPath").Return("path").Once()
	mockConfig.On("GetDocumentPath", "bucket", "path", mock.Anything, "pdf").Return("s3://document/path").Once()

	storageErr := errors.New("could not write")
	mockStorage.On("Write", mock.Anything, mock.Anything, "s3://document/path").Return(storageErr).Once()
	mockMetrics.
		On("RecordStorageUpload", t.Context(), matchesDocumentStorageUpload(telemetrymetrics.StorageUploadFailed)).
		Once()

	service := ServiceV1{
		config:  mockConfig,
		logger:  &MockLogger{},
		metrics: mockMetrics,
		storage: mockStorage,
	}

	writer := httptest.NewRecorder()

	body, contentType := createTestMultipartFormBuffer(t, []byte("my pdf data"), "application/pdf")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusInternalServerError, "failed to submit document")

	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
}

func TestServiceSubmitJobCreateError(t *testing.T) {
	mockConfig := new(MockConfig)
	mockMetrics := new(MockServiceMetrics)
	mockTicketRepository := new(MockTicketRepository)
	mockStorage := new(MockStorage)

	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockConfig.On("GetNotificationEmailLimit").Return(5).Once()
	mockConfig.On("GetBucket").Return("bucket").Once()
	mockConfig.On("GetBucketPath").Return("path").Once()
	mockConfig.On("GetDocumentPath", "bucket", "path", mock.Anything, "pdf").Return("s3://document/path").Once()
	mockStorage.On("Write", mock.Anything, mock.Anything, "s3://document/path").Return(nil).Once()
	mockMetrics.
		On("RecordStorageUpload", t.Context(), matchesDocumentStorageUpload(telemetrymetrics.StorageUploadSuccess)).
		Once()

	jobCreateError := errors.New("error creating job")
	mockTicketRepository.On("Create", t.Context(), mock.Anything).Return(jobCreateError).Once()

	service := ServiceV1{
		config:  mockConfig,
		job:     mockTicketRepository,
		logger:  &MockLogger{},
		metrics: mockMetrics,
		storage: mockStorage,
	}

	writer := httptest.NewRecorder()

	body, contentType := createTestMultipartFormBuffer(t, []byte("my pdf data"), "application/pdf")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusInternalServerError, "failed to submit document")

	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
	mockTicketRepository.AssertExpectations(t)
}

func TestServiceSubmitEnqueueError(t *testing.T) {
	mockConfig := new(MockConfig)
	mockMetrics := new(MockServiceMetrics)
	mockTicketRepository := new(MockTicketRepository)
	mockQueue := new(MockBatchQueue)
	mockStorage := new(MockStorage)

	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockConfig.On("GetNotificationEmailLimit").Return(5).Once()
	mockConfig.On("GetBucket").Return("bucket").Once()
	mockConfig.On("GetBucketPath").Return("path").Once()
	mockConfig.On("GetDocumentPath", "bucket", "path", mock.Anything, "pdf").Return("s3://document/path").Once()

	mockStorage.On("Write", mock.Anything, mock.Anything, "s3://document/path").Return(nil).Once()
	mockMetrics.
		On("RecordStorageUpload", t.Context(), matchesDocumentStorageUpload(telemetrymetrics.StorageUploadSuccess)).
		Once()
	mockTicketRepository.On("Create", t.Context(), mock.Anything).Return(nil).Once()

	queueError := errors.New("enqueue error")
	mockQueue.On("Enqueue", mock.Anything).Return(queueError).Once()
	mockTicketRepository.
		On(
			"MarkFailed",
			t.Context(),
			mock.AnythingOfType("uuid.UUID"),
			job.EnqueueFailed,
			"enqueue failed. err: enqueue error",
		).
		Return(true, nil).
		Once()

	service := ServiceV1{
		config:  mockConfig,
		job:     mockTicketRepository,
		logger:  &MockLogger{},
		metrics: mockMetrics,
		queue:   mockQueue,
		storage: mockStorage,
	}

	writer := httptest.NewRecorder()

	body, contentType := createTestMultipartFormBuffer(t, []byte("my pdf data"), "application/pdf")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusInternalServerError, "failed to submit document")

	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
	mockTicketRepository.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestServiceSubmitTooManyNotificationEmails(t *testing.T) {
	mockConfig := new(MockConfig)
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockConfig.On("GetNotificationEmailLimit").Return(1).Once()

	body, contentType := createTestMultipartFormBuffer(t, []byte("my pdf data"), "application/pdf")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	writer := httptest.NewRecorder()
	service := ServiceV1{config: mockConfig, logger: &MockLogger{}}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusBadRequest, "notification_emails must contain at most 1 entries")
	mockConfig.AssertExpectations(t)
}

func TestServiceSubmitInvalidNotificationEmails(t *testing.T) {
	mockConfig := new(MockConfig)
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()

	body, contentType := createTestMultipartFormBufferWithNotificationEmails(
		t,
		[]byte("my pdf data"),
		"application/pdf",
		"not-json",
	)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	writer := httptest.NewRecorder()
	service := ServiceV1{config: mockConfig, logger: &MockLogger{}}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assertAPIError(t, response, http.StatusBadRequest, "notification_emails must be a JSON array of strings")
	mockConfig.AssertExpectations(t)
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
			writer := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
			request.SetPathValue("id", tt.inputTicket)

			service := ServiceV1{logger: &MockLogger{}}
			service.status(writer, request)

			response := writer.Result()
			defer response.Body.Close()

			assertAPIError(t, response, http.StatusBadRequest, tt.expectedDetail)
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
			jobRepo := new(MockTicketRepository)
			jobRepo.On("Get", t.Context(), inputTicket).Return(nil, tt.getJobError).Once()

			writer := httptest.NewRecorder()
			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
			request.SetPathValue("id", inputTicket.String())

			service := ServiceV1{job: jobRepo, logger: &MockLogger{}}
			service.status(writer, request)

			response := writer.Result()
			defer response.Body.Close()

			assertAPIError(t, response, tt.expectedStatus, tt.expectedDetail)
			jobRepo.AssertExpectations(t)
		})
	}
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

			config := new(MockConfig)
			logger := new(MockLogger)
			jobRepo := new(MockTicketRepository)
			storage := new(MockStorage)

			w := httptest.NewRecorder()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
			r.SetPathValue("id", tt.inputTicket)

			service := ServiceV1{config: config, logger: logger, job: jobRepo, storage: storage}
			service.result(w, r)

			result := w.Result()
			defer result.Body.Close()

			assertAPIError(t, result, http.StatusBadRequest, tt.expectedDetail)
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

			config := new(MockConfig)
			logger := new(MockLogger)
			jobRepo := new(MockTicketRepository)
			storage := new(MockStorage)

			jobRepo.On("Get", t.Context(), inputTicket).Return(nil, tt.getJobError).Once()

			w := httptest.NewRecorder()
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
			r.SetPathValue("id", inputTicket.String())

			service := ServiceV1{config: config, logger: logger, job: jobRepo, storage: storage}
			service.result(w, r)

			result := w.Result()
			defer result.Body.Close()

			assertAPIError(t, result, tt.expectedStatus, tt.expectedDetail)

			jobRepo.AssertExpectations(t)
		})
	}
}

func TestServiceResultTicketNotComplete(t *testing.T) {
	inputTicket := uuid.New()

	jobRepo := new(MockTicketRepository)
	storage := new(MockStorage)
	ticket := job.NewTicket()
	ticket.ID = inputTicket
	jobRepo.On("Get", t.Context(), inputTicket).Return(ticket, nil).Once()

	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
	r.SetPathValue("id", inputTicket.String())

	service := ServiceV1{job: jobRepo, logger: new(MockLogger), storage: storage}
	service.result(w, r)

	result := w.Result()
	defer result.Body.Close()

	assert.Equal(t, http.StatusNoContent, result.StatusCode)
	assert.Empty(t, result.Header.Get("Content-Type"))
	responseBody, err := io.ReadAll(result.Body)
	require.NoError(t, err)
	assert.Empty(t, responseBody)
	jobRepo.AssertExpectations(t)
	storage.AssertNotCalled(t, "GetLink", mock.Anything, mock.Anything)
}

func TestServiceResultFailedGetLink(t *testing.T) {
	inputTicket := uuid.New()

	config := new(MockConfig)
	logger := new(MockLogger)
	jobRepo := new(MockTicketRepository)
	storage := new(MockStorage)

	ticket := job.NewTicket()
	ticket.Status = job.Complete
	jobRepo.On("Get", t.Context(), inputTicket).Return(ticket, nil).Once()
	storage.On("GetLink", t.Context(), mock.Anything).Return("", time.Now(), errors.New("failed to get link")).Once()
	config.On("GetBucket").Return("bucket")
	config.On("GetBucketPath").Return("path")
	config.On("GetOutputPath", "bucket", "path", ticket.ID.String()).Return("s3://bucket/path")

	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
	r.SetPathValue("id", inputTicket.String())

	service := ServiceV1{config: config, logger: logger, job: jobRepo, storage: storage}
	service.result(w, r)

	result := w.Result()
	defer result.Body.Close()

	assertAPIError(t, result, http.StatusInternalServerError, "failed to retrieve processing result")

	jobRepo.AssertExpectations(t)
	storage.AssertExpectations(t)
}

func TestServiceGetLinkSuccess(t *testing.T) {
	inputTicket := uuid.New()

	config := new(MockConfig)
	logger := new(MockLogger)
	jobRepo := new(MockTicketRepository)
	storage := new(MockStorage)

	expectedPresignedLink := "http://s3_presigned_link"
	expectedExpiration := time.Now().Add(time.Second * 60)

	ticket := job.NewTicket()
	ticket.Status = job.Complete
	jobRepo.On("Get", t.Context(), inputTicket).Return(ticket, nil).Once()
	storage.On("GetLink", t.Context(), mock.Anything).Return(expectedPresignedLink, expectedExpiration, nil).Once()
	config.On("GetBucket").Return("bucket")
	config.On("GetBucketPath").Return("path")
	config.On("GetOutputPath", "bucket", "path", ticket.ID.String()).Return("s3://bucket/path")

	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://test.com", http.NoBody)
	r.SetPathValue("id", inputTicket.String())

	service := ServiceV1{config: config, logger: logger, job: jobRepo, storage: storage}
	service.result(w, r)

	result := w.Result()
	require.Equal(t, http.StatusOK, result.StatusCode)

	b, err := io.ReadAll(result.Body)
	require.NoError(t, err)
	assert.Equal(t, "application/json", result.Header.Get("Content-Type"))
	assert.Contains(t, string(b), `"link":"`+expectedPresignedLink+`"`)
	assert.Contains(t, string(b), `"expires":"`+expectedExpiration.Format(time.RFC3339Nano)+`"`)

	var ro resultOutput
	require.NoError(t, json.Unmarshal(b, &ro))

	assert.Equal(t, expectedPresignedLink, ro.Link)
	assert.Equal(t, expectedExpiration.UnixMicro(), ro.Expires.UnixMicro())

	jobRepo.AssertExpectations(t)
	storage.AssertExpectations(t)
}

// HELPERS

type submitSuccessCase struct {
	name        string
	contentType string
	fileType    string
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
	mockTicketRepository := new(MockTicketRepository)
	mockQueue := new(MockBatchQueue)
	mockStorage := new(MockStorage)
	documentPath := "s3://document/path." + test.fileType

	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockConfig.On("GetNotificationEmailLimit").Return(5).Once()
	mockConfig.On("GetBucket").Return("bucket").Once()
	mockConfig.On("GetBucketPath").Return("path").Once()
	mockConfig.On("GetDocumentPath", "bucket", "path", mock.Anything, test.fileType).Return(documentPath).Once()

	mockStorage.
		On("Write", mock.Anything, mock.Anything, documentPath).
		Run(func(args mock.Arguments) {
			reader := args.Get(1).(io.Reader)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			assert.Equal(t, test.data, data)
		}).
		Return(nil).
		Once()
	mockMetrics.
		On("RecordStorageUpload", t.Context(), matchesDocumentStorageUpload(telemetrymetrics.StorageUploadSuccess)).
		Once()

	var generatedTicketID uuid.UUID
	mockTicketRepository.
		On("Create", t.Context(), mock.AnythingOfType("*job.Ticket")).
		Run(func(args mock.Arguments) {
			passedTicket := args.Get(1).(*job.Ticket)
			generatedTicketID = passedTicket.ID

			assert.Equal(t, documentPath, passedTicket.DocumentPath)
			assert.Equal(t, test.contentType, passedTicket.ContentType)
			assert.Equal(t, testNotificationEmails, passedTicket.NotificationEmails)
		}).
		Return(nil).
		Once()

	mockQueue.
		On("Enqueue", mock.AnythingOfType("*job.Ticket")).
		Run(func(args mock.Arguments) {
			ticket := args.Get(0).(*job.Ticket)
			assert.Equal(t, generatedTicketID, ticket.ID)
		}).
		Return(nil).
		Once()

	service := ServiceV1{
		config:  mockConfig,
		job:     mockTicketRepository,
		logger:  &MockLogger{},
		metrics: mockMetrics,
		queue:   mockQueue,
		storage: mockStorage,
	}

	body, requestContentType := createTestMultipartFormBuffer(t, test.data, test.contentType)
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", requestContentType)

	writer := httptest.NewRecorder()
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assert.Equal(t, http.StatusAccepted, response.StatusCode)
	assert.Equal(t, "application/json", response.Header.Get("Content-Type"))

	var ticket job.Ticket
	require.NoError(t, json.NewDecoder(response.Body).Decode(&ticket))

	assert.NotZero(t, ticket.ID)
	assert.NotZero(t, ticket.CreatedAt)
	assert.NotZero(t, ticket.UpdatedAt)
	assert.Equal(t, generatedTicketID, ticket.ID)
	assert.Equal(t, documentPath, ticket.DocumentPath)
	assert.Equal(t, test.contentType, ticket.ContentType)
	assert.Equal(t, testNotificationEmails, ticket.NotificationEmails)
	assert.Equal(t, job.Queued, ticket.Status)

	mockConfig.AssertExpectations(t)
	mockMetrics.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
	mockTicketRepository.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
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
	_ queue.BatchQueue         = (*MockBatchQueue)(nil)
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

func (m *MockConfig) GetServerReadTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *MockConfig) GetServerWriteTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *MockConfig) GetServerIdleTimeout() time.Duration {
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

func (m *MockConfig) GetOtelServiceName() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetOtelWorkerName() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetOtelServiceVersion() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetOtelMetricExportInterval() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

type MockTicketRepository struct {
	mock.Mock
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

type MockBatchQueue struct {
	mock.Mock
}

func (m *MockBatchQueue) Enqueue(ctx context.Context, ticket *job.Ticket) error {
	return m.Called(ticket).Error(0)
}

func (m *MockBatchQueue) Dequeue(ctx context.Context) (*queue.DequeueResult, error) {
	args := m.Called(ctx)

	if value := args.Get(0); value != nil {
		return value.(*queue.DequeueResult), args.Error(1)
	}

	return nil, args.Error(1)
}

func (m *MockBatchQueue) DeleteMessage(ctx context.Context, queueReference string) error {
	args := m.Called(ctx, queueReference)
	return args.Error(0)
}

func (m *MockBatchQueue) Ready(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

type MockStorage struct {
	mock.Mock
}

type MockServiceMetrics struct {
	mock.Mock
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

func matchesDocumentStorageUpload(outcome telemetrymetrics.StorageUploadOutcome) any {
	return mock.MatchedBy(func(observation telemetrymetrics.StorageUploadObservation) bool {
		return observation.Duration >= 0 &&
			observation.Purpose == telemetrymetrics.StorageUploadPurposeDocument &&
			observation.Outcome == outcome
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

func (m *MockLogger) Debug(message string, args ...any) {}

func (m *MockLogger) Info(message string, args ...any) {}

func (m *MockLogger) Warn(message string, args ...any) {}

func (m *MockLogger) Error(message string, args ...any) {}
