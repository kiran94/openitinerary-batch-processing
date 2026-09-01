package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

	assert.Equal(t, http.StatusRequestEntityTooLarge, response.StatusCode)
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

	b, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, string(b), "invalid multipart form")
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

	b, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, string(b), "no such file")

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

	b, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusBadRequest, response.StatusCode)
	assert.Contains(t, string(b), "Content-Type must be provided")
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

	assert.Equal(t, http.StatusUnsupportedMediaType, response.StatusCode)
	mockConfig.AssertExpectations(t)
}

func TestServiceSubmitUnsupportedFileFormat(t *testing.T) {
	mockConfig := new(MockConfig)
	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()

	body, contentType := createTestMultipartFormBuffer(t, []byte("image data"), "image/png")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	writer := httptest.NewRecorder()
	service := ServiceV1{config: mockConfig, logger: &MockLogger{}}
	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusUnsupportedMediaType, response.StatusCode)
	assert.Contains(t, string(responseBody), "unsupported file format: image/png")
	mockConfig.AssertExpectations(t)
}

func TestServiceSubmitStorageWriteError(t *testing.T) {
	mockConfig := new(MockConfig)
	mockStorage := new(MockStorage)

	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockConfig.On("GetBucket").Return("bucket").Once()
	mockConfig.On("GetBucketPath").Return("path").Once()
	mockConfig.On("GetDocumentPath", "bucket", "path", mock.Anything, "pdf").Return("s3://document/path").Once()

	storageErr := errors.New("could not write")
	mockStorage.On("Write", mock.Anything, mock.Anything, "s3://document/path").Return(storageErr).Once()

	service := ServiceV1{
		config:  mockConfig,
		logger:  &MockLogger{},
		storage: mockStorage,
	}

	writer := httptest.NewRecorder()

	body, contentType := createTestMultipartFormBuffer(t, []byte("my pdf data"), "application/pdf")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	b, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	assert.Contains(t, string(b), "failed to perform 'submit'")
	assert.Equal(t, http.StatusInternalServerError, response.StatusCode)

	mockConfig.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
}

func TestServiceSubmitJobCreateError(t *testing.T) {
	mockConfig := new(MockConfig)
	mockTicketRepository := new(MockTicketRepository)
	mockStorage := new(MockStorage)

	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockConfig.On("GetBucket").Return("bucket").Once()
	mockConfig.On("GetBucketPath").Return("path").Once()
	mockConfig.On("GetDocumentPath", "bucket", "path", mock.Anything, "pdf").Return("s3://document/path").Once()
	mockStorage.On("Write", mock.Anything, mock.Anything, "s3://document/path").Return(nil).Once()

	jobCreateError := errors.New("error creating job")
	mockTicketRepository.On("Create", t.Context(), mock.Anything).Return(jobCreateError).Once()

	service := ServiceV1{
		config:  mockConfig,
		job:     mockTicketRepository,
		logger:  &MockLogger{},
		storage: mockStorage,
	}

	writer := httptest.NewRecorder()

	body, contentType := createTestMultipartFormBuffer(t, []byte("my pdf data"), "application/pdf")
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/submit", body)
	request.Header.Set("Content-Type", contentType)

	service.submit(writer, request)

	response := writer.Result()
	defer response.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, response.StatusCode)

	mockConfig.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
	mockTicketRepository.AssertExpectations(t)
}

func TestServiceSubmitEnqueueError(t *testing.T) {
	mockConfig := new(MockConfig)
	mockTicketRepository := new(MockTicketRepository)
	mockQueue := new(MockBatchQueue)
	mockStorage := new(MockStorage)

	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
	mockConfig.On("GetBucket").Return("bucket").Once()
	mockConfig.On("GetBucketPath").Return("path").Once()
	mockConfig.On("GetDocumentPath", "bucket", "path", mock.Anything, "pdf").Return("s3://document/path").Once()

	mockStorage.On("Write", mock.Anything, mock.Anything, "s3://document/path").Return(nil).Once()
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

	assert.Equal(t, http.StatusInternalServerError, response.StatusCode)

	mockConfig.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
	mockTicketRepository.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

// HELPERS

type submitSuccessCase struct {
	name        string
	contentType string
	fileType    string
	data        []byte
}

func testServiceSubmitSuccess(t *testing.T, test submitSuccessCase) {
	t.Helper()

	mockConfig := new(MockConfig)
	mockTicketRepository := new(MockTicketRepository)
	mockQueue := new(MockBatchQueue)
	mockStorage := new(MockStorage)
	documentPath := "s3://document/path." + test.fileType

	mockConfig.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	mockConfig.On("GetMaximumBufferMultipartForm").Return(int64(5 << 20)).Once()
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

	var generatedTicketID uuid.UUID
	mockTicketRepository.
		On("Create", t.Context(), mock.AnythingOfType("*job.Ticket")).
		Run(func(args mock.Arguments) {
			passedTicket := args.Get(1).(*job.Ticket)
			generatedTicketID = passedTicket.ID

			assert.Equal(t, documentPath, passedTicket.DocumentPath)
			assert.Equal(t, test.contentType, passedTicket.ContentType)
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
	assert.Equal(t, job.Queued, ticket.Status)

	mockConfig.AssertExpectations(t)
	mockStorage.AssertExpectations(t)
	mockTicketRepository.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func createTestMultipartFormBuffer(t *testing.T, data []byte, contentType string) (*bytes.Buffer, string) {
	t.Helper()

	var body bytes.Buffer
	multipartWriter := multipart.NewWriter(&body)

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
	_ config.Config        = (*MockConfig)(nil)
	_ job.TicketRepository = (*MockTicketRepository)(nil)
	_ queue.BatchQueue     = (*MockBatchQueue)(nil)
	_ storage.Storage      = (*MockStorage)(nil)
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

func (m *MockConfig) GetWorkerTimeoutSeconds() int {
	return m.Called().Int(0)
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

type MockLogger struct{}

func (m *MockLogger) Debug(message string, args ...any) {}

func (m *MockLogger) Info(message string, args ...any) {}

func (m *MockLogger) Warn(message string, args ...any) {}

func (m *MockLogger) Error(message string, args ...any) {}
