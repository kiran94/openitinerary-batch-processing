package worker

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/clients"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/notification"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errConvert = errors.New("convert failed")
var errWrite = errors.New("write failed")
var errClose = errors.New("close failed")
var errQueue = errors.New("queue failed")
var errRepository = errors.New("repository failed")
var errRead = errors.New("read failed")

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
	notifier   *MockNotifier
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
		notifier:   new(MockNotifier),
		ticket:     ticket,
		delivery: queue.DequeueResult{
			Result:         queue.EnqueuePayload{TicketID: ticket.ID.String()},
			QueueReference: &receiptHandle,
		},
	}
	harness.worker = &BatchWorker{
		id:       uuid.New(),
		config:   harness.config,
		job:      harness.repository,
		queue:    harness.queue,
		storage:  harness.storage,
		adapter:  harness.adapter,
		logger:   &MockLogger{},
		notifier: harness.notifier,
	}

	t.Cleanup(func() {
		harness.config.AssertExpectations(t)
		harness.repository.AssertExpectations(t)
		harness.queue.AssertExpectations(t)
		harness.storage.AssertExpectations(t)
		harness.adapter.AssertExpectations(t)
		harness.notifier.AssertExpectations(t)
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

// Needed because newWorkerTestHarness installs default successful results on the repository mocks
func returnWriteError(err error) storageWriteFunc {
	return func(context.Context, io.Reader, string) error {
		return err
	}
}

func TestNewBatchWorker(t *testing.T) {
	harness := newWorkerTestHarness(t)

	worker := NewBatchWorker(
		harness.config,
		harness.repository,
		harness.queue,
		harness.storage,
		harness.adapter,
		&MockLogger{},
		harness.notifier,
	)

	require.NotNil(t, worker)
	assert.NotEmpty(t, worker.GetID())
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

func TestPollingContinuesWhenQueueIsEmpty(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.queue.On("Dequeue", t.Context()).Return(nil, nil).Once()
	harness.queue.On("Dequeue", t.Context()).Return(&harness.delivery, nil).Once()

	delivery, err := harness.worker.Polling(t.Context())

	require.NoError(t, err)
	assert.Equal(t, harness.delivery, *delivery)
	harness.queue.AssertNumberOfCalls(t, "Dequeue", 2)
}

func TestPollingReturnsQueueError(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.queue.On("Dequeue", t.Context()).Return(nil, errQueue).Once()

	delivery, err := harness.worker.Polling(t.Context())

	require.ErrorIs(t, err, errQueue)
	assert.Nil(t, delivery)
}

func TestPollingReturnsContextErrorFromDequeue(t *testing.T) {
	harness := newWorkerTestHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	harness.queue.
		On("Dequeue", ctx).
		Run(func(mock.Arguments) { cancel() }).
		Return(nil, errQueue).
		Once()

	delivery, err := harness.worker.Polling(ctx)

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, delivery)
}

func TestPollingDeletesCorruptDeliveryAndContinues(t *testing.T) {
	harness := newWorkerTestHarness(t)
	corruptDelivery := harness.delivery
	harness.queue.On("Dequeue", t.Context()).Return(&corruptDelivery, errQueue).Once()
	harness.queue.
		On("DeleteMessage", t.Context(), "receipt-handle").
		Return(nil).
		Once()
	harness.queue.On("Dequeue", t.Context()).Return(&harness.delivery, nil).Once()

	delivery, err := harness.worker.Polling(t.Context())

	require.NoError(t, err)
	assert.Equal(t, harness.delivery, *delivery)
	harness.queue.AssertNumberOfCalls(t, "Dequeue", 2)
}

func TestPollingReturnsCorruptDeliveryDeleteError(t *testing.T) {
	harness := newWorkerTestHarness(t)
	corruptDelivery := harness.delivery
	harness.queue.On("Dequeue", t.Context()).Return(&corruptDelivery, errQueue).Once()
	harness.queue.
		On("DeleteMessage", t.Context(), "receipt-handle").
		Return(errWrite).
		Once()

	delivery, err := harness.worker.Polling(t.Context())

	require.ErrorIs(t, err, errWrite)
	assert.Nil(t, delivery)
}

func TestPollingSkipsCorruptDeliveryWithoutQueueReference(t *testing.T) {
	harness := newWorkerTestHarness(t)
	corruptDelivery := queue.DequeueResult{} // No Queue Reference
	harness.queue.On("Dequeue", t.Context()).Return(&corruptDelivery, errQueue).Once()
	harness.queue.On("Dequeue", t.Context()).Return(&harness.delivery, nil).Once()

	delivery, err := harness.worker.Polling(t.Context())

	require.NoError(t, err)
	assert.Equal(t, harness.delivery, *delivery)
	harness.queue.AssertNotCalled(t, "DeleteMessage", mock.Anything, mock.Anything)
}

func TestStartDeletesInvalidTicketID(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.delivery.Result.TicketID = "invalid-ticket-id"
	harness.queue.
		On("DeleteMessage", t.Context(), "receipt-handle").
		Return(nil).
		Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.Error(t, err)
	harness.repository.AssertNotCalled(t, "Get", mock.Anything, mock.Anything)
	harness.storage.AssertNotCalled(t, "Read", mock.Anything, mock.Anything)
}

func TestStartReturnsInvalidTicketIDDeleteError(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.delivery.Result.TicketID = "invalid-ticket-id"
	harness.queue.
		On("DeleteMessage", t.Context(), "receipt-handle").
		Return(errWrite).
		Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.ErrorIs(t, err, errWrite)
	assert.Error(t, err)
}

func TestStartReturnsTicketGetError(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(nil, errRepository).Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.ErrorIs(t, err, errRepository)
	harness.repository.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	harness.storage.AssertNotCalled(t, "Read", mock.Anything, mock.Anything)
}

func TestStartDeletesAlreadyCompleteTicketMessage(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.ticket.Status = job.Complete
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.queue.
		On("DeleteMessage", t.Context(), "receipt-handle").
		Return(nil).
		Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.NoError(t, err)
	assert.Zero(t, harness.ticket.AttemptCount)
	harness.repository.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
	harness.storage.AssertNotCalled(t, "Read", mock.Anything, mock.Anything)
}

func TestStartReturnsAlreadyCompleteTicketDeleteError(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.ticket.Status = job.Complete
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.queue.
		On("DeleteMessage", t.Context(), "receipt-handle").
		Return(errWrite).
		Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.ErrorIs(t, err, ErrTicketAlreadyComplete)
	require.ErrorIs(t, err, errWrite)
	harness.repository.AssertNotCalled(t, "Update", mock.Anything, mock.Anything)
}

func TestStartReturnsProcessingUpdateError(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.repository.On("Update", t.Context(), harness.ticket).Return(errRepository).Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.ErrorIs(t, err, errRepository)
	assert.Equal(t, job.Processing, harness.ticket.Status)
	assert.Equal(t, uint16(1), harness.ticket.AttemptCount)
	harness.storage.AssertNotCalled(t, "Read", mock.Anything, mock.Anything)
}

func TestUpdateTicketAndErrorUsesDetachedBoundedContext(t *testing.T) {
	// Checks that parent context values are passed to cleanup context
	type contextKey struct{}

	harness := newWorkerTestHarness(t)
	parentCtx, cancel := context.WithCancel(context.WithValue(t.Context(), contextKey{}, "context-value"))
	cancel()

	var cleanupErr error
	var cleanupValue any
	var cleanupDeadline time.Time
	var hasCleanupDeadline bool
	harness.repository.
		On("Update", mock.Anything, harness.ticket).
		Run(func(args mock.Arguments) {
			cleanupCtx := args.Get(0).(context.Context)
			cleanupErr = cleanupCtx.Err()
			cleanupValue = cleanupCtx.Value(contextKey{})
			cleanupDeadline, hasCleanupDeadline = cleanupCtx.Deadline()
		}).
		Return(nil).
		Once()
	err := harness.worker.updateTicketAndError(parentCtx, harness.ticket, errRead, job.StorageReadFailed)

	require.ErrorIs(t, parentCtx.Err(), context.Canceled)
	require.ErrorIs(t, err, errRead)
	require.NoError(t, cleanupErr)
	assert.Equal(t, "context-value", cleanupValue)
	require.True(t, hasCleanupDeadline)
	remaining := time.Until(cleanupDeadline)
	assert.Positive(t, remaining)
	assert.LessOrEqual(t, remaining, time.Minute)
	harness.notifier.AssertNotCalled(t, "Notify", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestStartClosesSourceAndResponse(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.repository.On("Update", t.Context(), harness.ticket).Return(nil).Times(2)
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
	harness.notifier.On("Notify", mock.Anything, "Processing Success", mock.Anything, mock.Anything).Return(nil)

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.NoError(t, err)
	assert.True(t, source.closed)
	assert.True(t, response.closed)
	assert.JSONEq(t, `[{"type":"flight"}]`, written.String())
	assert.Equal(t, uint16(1), harness.ticket.AttemptCount)
	assert.Equal(t, job.Complete, harness.ticket.Status)
	assert.Nil(t, harness.ticket.Error)
	assert.Nil(t, harness.ticket.StatusReason)
	harness.repository.AssertNumberOfCalls(t, "Update", 2)
}

func TestStartClosesStreamsWhenConvertFails(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.repository.On("Update", mock.Anything, harness.ticket).Return(nil).Times(2)
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

func TestStartHandlesNilResponseWhenConvertFails(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.repository.On("Update", mock.Anything, harness.ticket).Return(nil).Times(2)
	source := &trackingReadCloser{Reader: bytes.NewBufferString("document")}
	harness.storage.
		On("Read", t.Context(), harness.ticket.DocumentPath).
		Return(source, nil).
		Once()
	harness.adapter.
		On("Convert", t.Context(), source, harness.ticket.DocumentPath, harness.ticket.ContentType).
		Return(nil, errConvert).
		Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.ErrorIs(t, err, errConvert)
	assert.True(t, source.closed)
	assertTicketFailure(t, harness, job.ConvertFailed, errConvert)
}

func TestStartClosesStreamsWhenWriteFails(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.repository.On("Update", mock.Anything, harness.ticket).Return(nil).Times(2)
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

func TestStartMarksTicketFailedWhenStorageReadFails(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.repository.On("Update", mock.Anything, harness.ticket).Return(nil).Times(2)
	harness.storage.
		On("Read", t.Context(), harness.ticket.DocumentPath).
		Return(nil, errRead).
		Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.ErrorIs(t, err, errRead)
	harness.adapter.AssertNotCalled(t, "Convert", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	assertTicketFailure(t, harness, job.StorageReadFailed, errRead)
}

func TestStartJoinsTicketFailureUpdateError(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.repository.On("Update", mock.Anything, harness.ticket).Return(nil).Once()
	harness.repository.On("Update", mock.Anything, harness.ticket).Return(errRepository).Once()
	harness.storage.
		On("Read", t.Context(), harness.ticket.DocumentPath).
		Return(nil, errRead).
		Once()

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.ErrorIs(t, err, errRead)
	require.ErrorIs(t, err, errRepository)
	require.NotNil(t, harness.ticket.StatusReason)
	assert.Equal(t, job.Failed, harness.ticket.Status)
	assert.Equal(t, job.StorageReadFailed, *harness.ticket.StatusReason)
	harness.repository.AssertNumberOfCalls(t, "Update", 2)
}

func TestStartRejectsNilSourceReader(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.repository.On("Update", mock.Anything, harness.ticket).Return(nil).Times(2)
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
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.repository.On("Update", mock.Anything, harness.ticket).Return(nil).Times(2)
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
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.repository.On("Update", t.Context(), harness.ticket).Return(nil).Times(2)
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
	harness.notifier.On("Notify", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.NoError(t, err)
	assert.True(t, source.closed)
	assert.True(t, response.closed)
}

func TestStartClassifiesOversizedAdapterResponse(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.repository.On("Update", mock.Anything, harness.ticket).Return(nil).Times(2)
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
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.repository.On("Update", t.Context(), harness.ticket).Return(nil).Times(2)
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
	assert.Equal(t, job.Complete, harness.ticket.Status)
	harness.repository.AssertNumberOfCalls(t, "Update", 2)
}

func TestStartReturnsCompletionUpdateError(t *testing.T) {
	harness := newWorkerTestHarness(t)
	harness.repository.On("Get", t.Context(), harness.ticket.ID).Return(harness.ticket, nil).Once()
	harness.repository.On("Update", t.Context(), harness.ticket).Return(nil).Once()
	harness.repository.On("Update", t.Context(), harness.ticket).Return(errRepository).Once()
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

	err := harness.worker.Start(t.Context(), harness.delivery)

	require.ErrorIs(t, err, errRepository)
	assert.True(t, source.closed)
	assert.True(t, response.closed)
	assert.Equal(t, job.Complete, harness.ticket.Status)
	harness.repository.AssertNumberOfCalls(t, "Update", 2)
	harness.queue.AssertNotCalled(t, "DeleteMessage", mock.Anything, mock.Anything)
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
	assert.Equal(t, uint16(1), harness.ticket.AttemptCount)
	assert.Equal(t, job.Failed, harness.ticket.Status)
	assert.Equal(t, expectedErr.Error(), *harness.ticket.Error)
	assert.Equal(t, reason, *harness.ticket.StatusReason)
	harness.repository.AssertNumberOfCalls(t, "Update", 2)
	harness.queue.AssertNotCalled(t, "DeleteMessage", mock.Anything, mock.Anything)
}

// MOCKS

var (
	_ config.Config         = (*MockConfig)(nil)
	_ job.TicketRepository  = (*MockTicketRepository)(nil)
	_ queue.BatchQueue      = (*MockBatchQueue)(nil)
	_ storage.Storage       = (*MockStorage)(nil)
	_ clients.Adapter       = (*MockAdapter)(nil)
	_ logging.Logger        = (*MockLogger)(nil)
	_ notification.Notifier = (*MockNotifier)(nil)
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

func (m *MockConfig) GetS3PresignedExpirationSeconds() int {
	return m.Called().Int(0)
}

func (m *MockConfig) GetOutputPath(bucket, bucketPath, jobID string) string {
	return m.Called(bucket, bucketPath, jobID).String(0)
}

func (m *MockConfig) GetNotificationEmailLimit() int {
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
	if value := args.Get(0); value != nil {
		return value.(*job.Ticket), args.Error(1)
	}

	return nil, args.Error(1)
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

func (m *MockBatchQueue) Ready(ctx context.Context) error {
	return m.Called(ctx).Error(0)
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

type MockNotifier struct {
	mock.Mock
}

func (m *MockNotifier) Notify(ctx context.Context, subject string, destination []string, content *string) error {
	return m.Called(ctx, subject, destination, content).Error(0)
}
