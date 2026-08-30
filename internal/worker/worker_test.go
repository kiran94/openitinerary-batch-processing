package worker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/clients"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errConvert = errors.New("convert failed")
var errWrite = errors.New("write failed")
var errClose = errors.New("close failed")

type trackingReadCloser struct {
	io.Reader
	closed   bool
	closeErr error
}

type storageWriteFunc func(context.Context, io.Reader, string) error

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return r.closeErr
}

type workerTestHarness struct {
	worker     *BatchWorker
	config     *MockConfig
	repository *MockTicketRepository
	queue      *MockBatchQueue
	storage    *MockStorage
	adapter    *MockAdapter
	ticket     *job.Ticket
	delivery   queue.DequeueResult
}

func newWorkerTestHarness(t *testing.T) *workerTestHarness {
	t.Helper()

	ticket := job.NewTicket()
	ticket.DocumentPath = "s3://bucket/document.pdf"
	ticket.ContentType = "application/pdf"
	receiptHandle := "receipt-handle"

	harness := &workerTestHarness{
		config:     new(MockConfig),
		repository: new(MockTicketRepository),
		queue:      new(MockBatchQueue),
		storage:    new(MockStorage),
		adapter:    new(MockAdapter),
		ticket:     ticket,
		delivery: queue.DequeueResult{
			Result:         queue.EnqueuePayload{TicketID: ticket.ID.String()},
			QueueReference: &receiptHandle,
		},
	}
	harness.worker = &BatchWorker{
		id:      uuid.New(),
		config:  harness.config,
		job:     harness.repository,
		queue:   harness.queue,
		storage: harness.storage,
		adapter: harness.adapter,
		logger:  &MockLogger{},
	}

	harness.repository.On("Get", ticket.ID).Return(ticket, nil).Maybe()
	harness.repository.On("Update", ticket).Return(nil).Maybe()

	t.Cleanup(func() {
		harness.config.AssertExpectations(t)
		harness.repository.AssertExpectations(t)
		harness.queue.AssertExpectations(t)
		harness.storage.AssertExpectations(t)
		harness.adapter.AssertExpectations(t)
	})

	return harness
}

func (h *workerTestHarness) expectOutputWrite(
	ctx context.Context,
	response io.Reader,
	write storageWriteFunc,
) {
	h.config.On("GetBucket").Return("bucket").Once()
	h.config.On("GetBucketPath").Return("prefix").Once()
	h.config.
		On("GetOutputPath", "bucket", "prefix", h.ticket.ID.String()).
		Return("s3://bucket/result.json").
		Once()
	h.storage.
		On("Write", ctx, response, "s3://bucket/result.json").
		Return(write).
		Once()
}

func returnWriteError(err error) storageWriteFunc {
	return func(context.Context, io.Reader, string) error {
		return err
	}
}

func TestPollingReturnsDelivery(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.queue.On("Dequeue", t.Context()).Return(&harness.delivery, nil).Once()

	delivery, err := harness.worker.Polling(t.Context())

	require.NoError(t, err)
	assert.Equal(t, harness.delivery, *delivery)
}

func TestPollingReturnsCancelledContext(t *testing.T) {
	harness := newWorkerTestHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	delivery, err := harness.worker.Polling(ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, delivery)
	harness.queue.AssertNotCalled(t, "Dequeue", mock.Anything)
}

func TestStartClosesSourceAndResponse(t *testing.T) {
	harness := newWorkerTestHarness(t)
	source := &trackingReadCloser{Reader: bytes.NewBufferString("document")}
	response := &trackingReadCloser{Reader: bytes.NewBufferString(`[{"type":"flight"}]`)}
	var written bytes.Buffer

	harness.storage.
		On("Read", t.Context(), harness.ticket.DocumentPath).
		Return(source, nil).
		Once()
	harness.adapter.
		On("Convert", t.Context(), source, harness.ticket.DocumentPath, harness.ticket.ContentType).
		Return(response, nil).
		Once()
	harness.expectOutputWrite(
		t.Context(),
		response,
		func(_ context.Context, reader io.Reader, _ string) error {
			_, err := io.Copy(&written, reader)
			return err
		},
	)
	harness.queue.
		On("DeleteMessage", t.Context(), "receipt-handle").
		Return(nil).
		Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.NoError(t, err)
	assert.True(t, source.closed)
	assert.True(t, response.closed)
	assert.JSONEq(t, `[{"type":"flight"}]`, written.String())
	assert.Equal(t, 1, harness.ticket.AttemptCount)
	assert.Equal(t, job.Complete, harness.ticket.Status)
	assert.Nil(t, harness.ticket.Error)
	assert.Nil(t, harness.ticket.StatusReason)
	harness.repository.AssertNumberOfCalls(t, "Update", 2)
}

func TestStartClosesStreamsWhenConvertFails(t *testing.T) {
	harness := newWorkerTestHarness(t)
	source := &trackingReadCloser{Reader: bytes.NewBufferString("document")}
	response := &trackingReadCloser{Reader: bytes.NewBufferString("unused")}
	harness.storage.
		On("Read", t.Context(), harness.ticket.DocumentPath).
		Return(source, nil).
		Once()
	harness.adapter.
		On("Convert", t.Context(), source, harness.ticket.DocumentPath, harness.ticket.ContentType).
		Return(response, errConvert).
		Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.ErrorIs(t, err, errConvert)
	assert.True(t, source.closed)
	assert.True(t, response.closed)
	assertTicketFailure(t, harness, job.ConvertFailed, errConvert)
}

func TestStartClosesStreamsWhenWriteFails(t *testing.T) {
	harness := newWorkerTestHarness(t)
	source := &trackingReadCloser{Reader: bytes.NewBufferString("document")}
	response := &trackingReadCloser{Reader: bytes.NewBufferString("result")}
	harness.storage.
		On("Read", t.Context(), harness.ticket.DocumentPath).
		Return(source, nil).
		Once()
	harness.adapter.
		On("Convert", t.Context(), source, harness.ticket.DocumentPath, harness.ticket.ContentType).
		Return(response, nil).
		Once()
	harness.expectOutputWrite(t.Context(), response, returnWriteError(errWrite))

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.ErrorIs(t, err, errWrite)
	assert.True(t, source.closed)
	assert.True(t, response.closed)
	assertTicketFailure(t, harness, job.StorageWriteFailed, errWrite)
}

func TestStartRejectsNilSourceReader(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.storage.
		On("Read", t.Context(), harness.ticket.DocumentPath).
		Return(nil, nil).
		Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil document reader")
	harness.adapter.AssertNotCalled(t, "Convert", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	assertTicketFailure(t, harness, job.StorageReadNil, err)
}

func TestStartRejectsNilAdapterResponse(t *testing.T) {
	harness := newWorkerTestHarness(t)
	source := &trackingReadCloser{Reader: bytes.NewBufferString("document")}
	harness.storage.
		On("Read", t.Context(), harness.ticket.DocumentPath).
		Return(source, nil).
		Once()
	harness.adapter.
		On("Convert", t.Context(), source, harness.ticket.DocumentPath, harness.ticket.ContentType).
		Return(nil, nil).
		Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil response reader")
	assert.True(t, source.closed)
	assertTicketFailure(t, harness, job.ResponseReaderNil, err)
}

func TestStartIgnoresStreamCloseErrors(t *testing.T) {
	harness := newWorkerTestHarness(t)
	source := &trackingReadCloser{Reader: bytes.NewBufferString("document"), closeErr: errClose}
	response := &trackingReadCloser{Reader: bytes.NewBufferString("result"), closeErr: errClose}
	harness.storage.
		On("Read", t.Context(), harness.ticket.DocumentPath).
		Return(source, nil).
		Once()
	harness.adapter.
		On("Convert", t.Context(), source, harness.ticket.DocumentPath, harness.ticket.ContentType).
		Return(response, nil).
		Once()
	harness.expectOutputWrite(t.Context(), response, returnWriteError(nil))
	harness.queue.
		On("DeleteMessage", t.Context(), "receipt-handle").
		Return(nil).
		Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.NoError(t, err)
	assert.True(t, source.closed)
	assert.True(t, response.closed)
}

func TestStartClassifiesOversizedAdapterResponse(t *testing.T) {
	harness := newWorkerTestHarness(t)
	source := &trackingReadCloser{Reader: bytes.NewBufferString("document")}
	responseBody := &trackingReadCloser{Reader: bytes.NewBufferString("123456")}
	response := http.MaxBytesReader(nil, responseBody, 5)
	harness.storage.
		On("Read", t.Context(), harness.ticket.DocumentPath).
		Return(source, nil).
		Once()
	harness.adapter.
		On("Convert", t.Context(), source, harness.ticket.DocumentPath, harness.ticket.ContentType).
		Return(response, nil).
		Once()
	harness.expectOutputWrite(
		t.Context(),
		response,
		func(_ context.Context, reader io.Reader, _ string) error {
			_, err := io.Copy(io.Discard, reader)
			return err
		},
	)

	err := harness.worker.Start(t.Context(), harness.delivery)

	var maxBytesErr *http.MaxBytesError
	require.ErrorAs(t, err, &maxBytesErr)
	assert.Equal(t, int64(5), maxBytesErr.Limit)
	assert.True(t, responseBody.closed)
	assertTicketFailure(t, harness, job.StorageWriteFailed, err)
}

func TestStartReturnsDeleteMessageError(t *testing.T) {
	harness := newWorkerTestHarness(t)
	source := &trackingReadCloser{Reader: bytes.NewBufferString("document")}
	response := &trackingReadCloser{Reader: bytes.NewBufferString("result")}
	harness.storage.
		On("Read", t.Context(), harness.ticket.DocumentPath).
		Return(source, nil).
		Once()
	harness.adapter.
		On("Convert", t.Context(), source, harness.ticket.DocumentPath, harness.ticket.ContentType).
		Return(response, nil).
		Once()
	harness.expectOutputWrite(t.Context(), response, returnWriteError(nil))
	harness.queue.
		On("DeleteMessage", t.Context(), "receipt-handle").
		Return(errWrite).
		Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.ErrorIs(t, err, errWrite)
}

func assertTicketFailure(
	t *testing.T,
	harness *workerTestHarness,
	reason job.TicketStatusReason,
	expectedErr error,
) {
	t.Helper()

	require.NotNil(t, harness.ticket.Error)
	require.NotNil(t, harness.ticket.StatusReason)
	assert.Equal(t, 1, harness.ticket.AttemptCount)
	assert.Equal(t, job.Failed, harness.ticket.Status)
	assert.Equal(t, expectedErr.Error(), *harness.ticket.Error)
	assert.Equal(t, reason, *harness.ticket.StatusReason)
	harness.repository.AssertNumberOfCalls(t, "Update", 2)
	harness.queue.AssertNotCalled(t, "DeleteMessage", mock.Anything, mock.Anything)
}

// MOCKS

var (
	_ config.Config        = (*MockConfig)(nil)
	_ job.TicketRepository = (*MockTicketRepository)(nil)
	_ queue.BatchQueue     = (*MockBatchQueue)(nil)
	_ storage.Storage      = (*MockStorage)(nil)
	_ clients.Adapter      = (*MockAdapter)(nil)
	_ logging.Logger       = (*MockLogger)(nil)
)

type MockConfig struct {
	config.ApplicationConfig
	mock.Mock
}

func (m *MockConfig) GetBucket() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetBucketPath() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetOutputPath(bucket, bucketPath, jobID string) string {
	return m.Called(bucket, bucketPath, jobID).String(0)
}

type MockTicketRepository struct {
	mock.Mock
}

func (m *MockTicketRepository) Create(ticket *job.Ticket) error {
	return m.Called(ticket).Error(0)
}

func (m *MockTicketRepository) Get(id uuid.UUID) (*job.Ticket, error) {
	args := m.Called(id)
	if value := args.Get(0); value != nil {
		return value.(*job.Ticket), args.Error(1)
	}

	return nil, args.Error(1)
}

func (m *MockTicketRepository) Update(ticket *job.Ticket) error {
	return m.Called(ticket).Error(0)
}

func (m *MockTicketRepository) MarkFailed(
	id uuid.UUID,
	reason job.TicketStatusReason,
	detail string,
) (bool, error) {
	args := m.Called(id, reason, detail)
	return args.Bool(0), args.Error(1)
}

type MockBatchQueue struct {
	mock.Mock
}

func (m *MockBatchQueue) Enqueue(ctx context.Context, ticket *job.Ticket) error {
	return m.Called(ctx, ticket).Error(0)
}

func (m *MockBatchQueue) Dequeue(ctx context.Context) (*queue.DequeueResult, error) {
	args := m.Called(ctx)
	if value := args.Get(0); value != nil {
		return value.(*queue.DequeueResult), args.Error(1)
	}

	return nil, args.Error(1)
}

func (m *MockBatchQueue) DeleteMessage(ctx context.Context, queueReference string) error {
	return m.Called(ctx, queueReference).Error(0)
}

type MockStorage struct {
	mock.Mock
}

func (m *MockStorage) Read(ctx context.Context, input string) (io.ReadCloser, error) {
	args := m.Called(ctx, input)
	if value := args.Get(0); value != nil {
		return value.(io.ReadCloser), args.Error(1)
	}

	return nil, args.Error(1)
}

func (m *MockStorage) Write(ctx context.Context, reader io.Reader, output string) error {
	args := m.Called(ctx, reader, output)
	write := args.Get(0).(storageWriteFunc)
	return write(ctx, reader, output)
}

type MockAdapter struct {
	mock.Mock
}

func (m *MockAdapter) Convert(
	ctx context.Context,
	document io.Reader,
	filename string,
	contentType string,
) (io.ReadCloser, error) {
	args := m.Called(ctx, document, filename, contentType)
	if value := args.Get(0); value != nil {
		return value.(io.ReadCloser), args.Error(1)
	}

	return nil, args.Error(1)
}

type MockLogger struct{}

func (*MockLogger) Debug(string, ...any) {}
func (*MockLogger) Info(string, ...any)  {}
func (*MockLogger) Warn(string, ...any)  {}
func (*MockLogger) Error(string, ...any) {}
