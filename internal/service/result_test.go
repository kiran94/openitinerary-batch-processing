package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/kiran94/openitinerary-batch-processing/internal/testutils"
	"github.com/kiran94/openitinerary-batch-processing/pkg/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGetResultLinkTicketLookupError(t *testing.T) {
	tests := []struct {
		name           string
		ticketGetError error
	}{
		{name: "ticket not found", ticketGetError: job.ErrRecordNotFound},
		{name: "ticket lookup failed", ticketGetError: errors.New("failed to get ticket")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newResultTestHarness(t)
			ticketID := uuid.New()
			harness.jobs.
				On("Get", t.Context(), ticketID).
				Return(nil, tt.ticketGetError).
				Once()

			result, err := harness.resultService.GetResultLink(t.Context(), ticketID)

			require.ErrorIs(t, err, tt.ticketGetError)
			assert.Nil(t, result)

			harness.storage.AssertNotCalled(t, "GetLink", mock.Anything, mock.Anything)
			harness.assertExpectations(t)
		})
	}
}

func TestGetResultLinkJobNotComplete(t *testing.T) {
	harness := newResultTestHarness(t)
	ticketID := uuid.New()
	ticket := job.NewTicket()
	ticket.ID = ticketID
	harness.jobs.
		On("Get", t.Context(), ticketID).
		Return(ticket, nil).
		Once()

	result, err := harness.resultService.GetResultLink(t.Context(), ticketID)

	require.ErrorIs(t, err, ErrJobNotComplete)
	assert.Nil(t, result)
	assert.False(t, harness.config.outputPathRequested)

	harness.storage.AssertNotCalled(t, "GetLink", mock.Anything, mock.Anything)
	harness.assertExpectations(t)
}

func TestGetResultLinkStorageError(t *testing.T) {
	harness := newResultTestHarness(t)
	ticketID := uuid.New()
	ticket := job.NewTicket()
	ticket.ID = ticketID
	ticket.Status = job.Complete
	storageError := errors.New("failed to get link")

	harness.config.expectedTicketID = ticketID
	harness.jobs.
		On("Get", t.Context(), ticketID).
		Return(ticket, nil).
		Once()
	harness.storage.
		On("GetLink", t.Context(), harness.config.outputPath).
		Return("", time.Time{}, storageError).
		Once()

	result, err := harness.resultService.GetResultLink(t.Context(), ticketID)

	require.ErrorIs(t, err, storageError)
	assert.Nil(t, result)
	assert.True(t, harness.config.outputPathRequested)

	harness.assertExpectations(t)
}

func TestGetResultLinkSuccess(t *testing.T) {
	harness := newResultTestHarness(t)
	ticketID := uuid.New()
	ticket := job.NewTicket()
	ticket.ID = ticketID
	ticket.Status = job.Complete
	expectedLink := "http://s3_presigned_link"
	expectedExpiration := time.Now().UTC().Add(time.Minute)

	harness.config.expectedTicketID = ticketID
	harness.jobs.
		On("Get", t.Context(), ticketID).
		Return(ticket, nil).
		Once()
	harness.storage.
		On("GetLink", t.Context(), harness.config.outputPath).
		Return(expectedLink, expectedExpiration, nil).
		Once()

	result, err := harness.resultService.GetResultLink(t.Context(), ticketID)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, expectedLink, result.Link)
	assert.Equal(t, expectedExpiration, result.Expires)
	assert.True(t, harness.config.outputPathRequested)

	harness.assertExpectations(t)
}

type resultTestHarness struct {
	config        *resultTestConfig
	jobs          *resultTicketRepositoryMock
	storage       *resultStorageMock
	resultService ResultService
}

func newResultTestHarness(t *testing.T) *resultTestHarness {
	t.Helper()

	configuration := &resultTestConfig{
		t:          t,
		bucket:     "bucket",
		bucketPath: "path",
		outputPath: "s3://bucket/path/result.json",
	}
	jobs := new(resultTicketRepositoryMock)
	storageClient := new(resultStorageMock)

	return &resultTestHarness{
		config:  configuration,
		jobs:    jobs,
		storage: storageClient,
		resultService: NewResultService(
			configuration,
			&resultLogger{},
			testutils.NewMockTracer(),
			jobs,
			storageClient,
		),
	}
}

func (h *resultTestHarness) assertExpectations(t *testing.T) {
	t.Helper()

	h.jobs.AssertExpectations(t)
	h.storage.AssertExpectations(t)
}

type resultTestConfig struct {
	config.Config
	t                   *testing.T
	bucket              string
	bucketPath          string
	outputPath          string
	expectedTicketID    uuid.UUID
	outputPathRequested bool
}

func (c *resultTestConfig) GetBucket() string {
	return c.bucket
}

func (c *resultTestConfig) GetBucketPath() string {
	return c.bucketPath
}

func (c *resultTestConfig) GetOutputPath(bucket, bucketPath, ticketID string) string {
	assert.Equal(c.t, c.bucket, bucket)
	assert.Equal(c.t, c.bucketPath, bucketPath)
	assert.Equal(c.t, c.expectedTicketID.String(), ticketID)
	c.outputPathRequested = true
	return c.outputPath
}

type resultTicketRepositoryMock struct {
	job.TicketRepository
	mock.Mock
}

func (m *resultTicketRepositoryMock) Get(ctx context.Context, id uuid.UUID) (*job.Ticket, error) {
	args := m.Called(ctx, id)
	if ticket := args.Get(0); ticket != nil {
		return ticket.(*job.Ticket), args.Error(1)
	}
	return nil, args.Error(1)
}

type resultStorageMock struct {
	storage.Storage
	mock.Mock
}

func (m *resultStorageMock) GetLink(ctx context.Context, input string) (string, time.Time, error) {
	args := m.Called(ctx, input)
	return args.String(0), args.Get(1).(time.Time), args.Error(2)
}

type resultLogger struct{}

func (*resultLogger) Debug(context.Context, string, ...any) {}

func (*resultLogger) Info(context.Context, string, ...any) {}

func (*resultLogger) Warn(context.Context, string, ...any) {}

func (*resultLogger) Error(context.Context, string, ...any) {}

func (l *resultLogger) WithContext(context.Context) logging.Logger { return l }

var (
	_ config.Config        = (*resultTestConfig)(nil)
	_ job.TicketRepository = (*resultTicketRepositoryMock)(nil)
	_ storage.Storage      = (*resultStorageMock)(nil)
	_ logging.Logger       = (*resultLogger)(nil)
)
