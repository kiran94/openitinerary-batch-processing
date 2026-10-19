package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/worker"
	"github.com/kiran94/openitinerary-batch-processing/pkg/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errStartWorkPolling = errors.New("polling failed")
var errStartWorkProcessing = errors.New("processing failed")

type startWorkTestHarness struct {
	worker   *mockStartWorkWorker
	logger   *mockLogger
	config   config.ApplicationConfig
	delivery queue.DequeueResult
}

func newStartWorkTestHarness(t *testing.T) *startWorkTestHarness {
	t.Helper()
	t.Setenv("OIBP_WORKER_TIMEOUT_SECONDS", "30")

	harness := &startWorkTestHarness{
		worker: new(mockStartWorkWorker),
		logger: new(mockLogger),
		config: config.ApplicationConfig{},
		delivery: queue.DequeueResult{
			Result: queue.EnqueuePayload{TicketID: "ticket-id"},
		},
	}

	t.Cleanup(func() {
		harness.worker.AssertExpectations(t)
	})

	return harness
}

func TestStartWorkProcessesDelivery(t *testing.T) {
	harness := newStartWorkTestHarness(t)
	harness.worker.On("Polling", mock.Anything).Return(&harness.delivery, nil).Once()
	harness.worker.On("Start", mock.Anything, harness.delivery).Return(nil).Once()

	processedItem, err := startWork(
		t.Context(),
		harness.worker,
		harness.config,
		harness.logger,
	)

	require.NoError(t, err)
	assert.True(t, processedItem)
}

func TestStartWorkReturnsPollingErrorBeforeProcessing(t *testing.T) {
	harness := newStartWorkTestHarness(t)
	harness.worker.On("Polling", mock.Anything).Return(nil, errStartWorkPolling).Once()

	processedItem, err := startWork(
		t.Context(),
		harness.worker,
		harness.config,
		harness.logger,
	)

	require.ErrorIs(t, err, errStartWorkPolling)
	assert.False(t, processedItem)
	harness.worker.AssertNotCalled(t, "Start", mock.Anything, mock.Anything)
}

func TestStartWorkReturnsProcessingErrorAfterPickingUpDelivery(t *testing.T) {
	harness := newStartWorkTestHarness(t)
	harness.worker.On("Polling", mock.Anything).Return(&harness.delivery, nil).Once()
	harness.worker.
		On("Start", mock.Anything, harness.delivery).
		Return(errStartWorkProcessing).
		Once()

	processedItem, err := startWork(
		t.Context(),
		harness.worker,
		harness.config,
		harness.logger,
	)

	require.ErrorIs(t, err, errStartWorkProcessing)
	assert.True(t, processedItem)
}

func TestStartWorkReturnsDeadlineExceededBeforePickingUpDelivery(t *testing.T) {
	harness := newStartWorkTestHarness(t)
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()

	harness.worker.
		On("Polling", mock.MatchedBy(func(ctx context.Context) bool {
			return errors.Is(ctx.Err(), context.DeadlineExceeded)
		})).
		Return(nil, context.DeadlineExceeded).
		Once()

	processedItem, err := startWork(
		ctx,
		harness.worker,
		harness.config,
		harness.logger,
	)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.False(t, processedItem)
	harness.worker.AssertNotCalled(t, "Start", mock.Anything, mock.Anything)
}

type mockLogger struct{}

func (*mockLogger) Debug(context.Context, string, ...any)        {}
func (*mockLogger) Info(context.Context, string, ...any)         {}
func (*mockLogger) Warn(context.Context, string, ...any)         {}
func (*mockLogger) Error(context.Context, string, ...any)        {}
func (m *mockLogger) WithContext(context.Context) logging.Logger { return m }

var (
	_ worker.Worker  = (*mockStartWorkWorker)(nil)
	_ logging.Logger = (*mockLogger)(nil)
)

type mockStartWorkWorker struct {
	mock.Mock
}

func (m *mockStartWorkWorker) GetID() string {
	return "worker-id"
}

func (m *mockStartWorkWorker) Polling(ctx context.Context) (*queue.DequeueResult, error) {
	args := m.Called(ctx)
	if value := args.Get(0); value != nil {
		return value.(*queue.DequeueResult), args.Error(1)
	}

	return nil, args.Error(1)
}

func (m *mockStartWorkWorker) Start(ctx context.Context, delivery queue.DequeueResult) error {
	return m.Called(ctx, delivery).Error(0)
}
