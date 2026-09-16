package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/queue"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry/metrics"
	"github.com/kiran94/openitinerary-batch-processing/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestSubmitSuccess(t *testing.T) {
	tests := []struct {
		name        string
		contentType config.ContentType
		fileType    string
		document    []byte
	}{
		{
			name:        "PDF",
			contentType: config.ContentTypePDF,
			fileType:    "pdf",
			document:    []byte("my pdf data"),
		},
		{
			name:        "HTML",
			contentType: config.ContentTypeHTML,
			fileType:    "html",
			document:    []byte("<html><body>test</body></html>"),
		},
		{
			name:        "Email",
			contentType: config.ContentTypeEmail,
			fileType:    "eml",
			document:    []byte("Subject: Test\r\n\r\nEmail body"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newSubmitTestHarness(t)
			harness.config.expectedFileType = tt.fileType
			harness.config.documentPath = "s3://document/path." + tt.fileType
			notificationEmails := []string{"traveller@example.com", "companion@example.com"}
			documentReader := bytes.NewReader(tt.document)

			harness.storage.
				On("Write", submitContextHasTicketID(), documentReader, harness.config.documentPath).
				Run(func(args mock.Arguments) {
					actualDocument, err := io.ReadAll(args.Get(1).(io.Reader))
					require.NoError(t, err)
					assert.Equal(t, tt.document, actualDocument)
				}).
				Return(nil).
				Once()
			harness.metrics.
				On("RecordStorageUpload", submitContextHasTicketID(), matchesDocumentStorageUpload(metrics.StorageUploadSuccess)).
				Once()

			var createdTicket *job.Ticket
			harness.jobs.
				On("Create", submitContextHasTicketID(), mock.AnythingOfType("*job.Ticket")).
				Run(func(args mock.Arguments) {
					createdTicket = args.Get(1).(*job.Ticket)
				}).
				Return(nil).
				Once()
			harness.queue.
				On("Enqueue", submitContextHasTicketID(), mock.AnythingOfType("*job.Ticket")).
				Run(func(args mock.Arguments) {
					assert.Same(t, createdTicket, args.Get(1).(*job.Ticket))
				}).
				Return(nil).
				Once()

			input := SubmitInput{
				Document:           documentReader,
				SizeBytes:          int64(len(tt.document)),
				ContentType:        tt.contentType,
				NotificationEmails: notificationEmails,
				SubmitStartedAt:    time.Now().UTC(),
			}
			ticket, err := harness.submitter.Submit(t.Context(), input)

			require.NoError(t, err)
			require.NotNil(t, ticket)
			assert.Same(t, createdTicket, ticket)
			assert.NotEqual(t, uuid.Nil, ticket.ID)
			assert.Equal(t, harness.config.documentPath, ticket.DocumentPath)
			assert.Equal(t, string(tt.contentType), ticket.ContentType)
			assert.Equal(t, notificationEmails, ticket.NotificationEmails)
			assert.Equal(t, job.Queued, ticket.Status)
			assert.False(t, ticket.CreatedAt.IsZero())
			assert.False(t, ticket.UpdatedAt.IsZero())

			harness.assertExpectations(t)
		})
	}
}

func TestSubmitUnsupportedContentType(t *testing.T) {
	harness := newSubmitTestHarness(t)
	input := SubmitInput{
		Document:        bytes.NewReader([]byte("image data")),
		SizeBytes:       int64(len("image data")),
		ContentType:     config.ContentType("image/png"),
		SubmitStartedAt: time.Now().UTC(),
	}

	ticket, err := harness.submitter.Submit(t.Context(), input)

	require.EqualError(t, err, "could not convert content type 'image/png' to a file type")
	assert.Nil(t, ticket)
	assert.Empty(t, harness.config.receivedFileType)

	harness.assertExpectations(t)
}

func TestSubmitTooManyNotificationEmails(t *testing.T) {
	harness := newSubmitTestHarness(t)
	harness.config.notificationEmailLimit = 1
	expectedContentType := config.ContentTypePDF
	harness.metrics.
		On(
			"RecordSubmission",
			submitContextHasTicketID(),
			metrics.SubmissionObservation{
				Outcome:     metrics.SubmissionRejected,
				Reason:      new(metrics.SubmissionReasonInvalidNotificationEmails),
				ContentType: &expectedContentType,
			},
		).
		Once()

	input := SubmitInput{
		Document:           bytes.NewReader([]byte("my pdf data")),
		SizeBytes:          int64(len("my pdf data")),
		ContentType:        expectedContentType,
		NotificationEmails: []string{"one@example.com", "two@example.com"},
		SubmitStartedAt:    time.Now().UTC(),
	}
	ticket, err := harness.submitter.Submit(t.Context(), input)

	var invalidEmailsError ErrInvalidNotificationEmails
	require.ErrorAs(t, err, &invalidEmailsError)
	require.EqualError(t, err, "notification_emails must contain at most 1 entries")
	assert.Nil(t, ticket)

	harness.storage.AssertNotCalled(t, "Write", mock.Anything, mock.Anything, mock.Anything)
	harness.jobs.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	harness.queue.AssertNotCalled(t, "Enqueue", mock.Anything, mock.Anything)
	harness.assertExpectations(t)
}

func TestSubmitStorageWriteError(t *testing.T) {
	harness := newSubmitTestHarness(t)
	document := []byte("my pdf data")
	expectedContentType := config.ContentTypePDF
	expectedInputBytes := int64(len(document))
	storageError := errors.New("could not write")

	harness.storage.
		On("Write", submitContextHasTicketID(), mock.Anything, harness.config.documentPath).
		Return(storageError).
		Once()
	harness.metrics.
		On("RecordStorageUpload", submitContextHasTicketID(), matchesDocumentStorageUpload(metrics.StorageUploadFailed)).
		Once()
	harness.metrics.
		On(
			"RecordSubmission",
			submitContextHasTicketID(),
			metrics.SubmissionObservation{
				Outcome:     metrics.SubmissionFailed,
				Reason:      new(metrics.SubmissionReasonStorageWriteFailed),
				ContentType: &expectedContentType,
				InputBytes:  &expectedInputBytes,
			},
		).
		Once()

	ticket, err := harness.submitter.Submit(t.Context(), SubmitInput{
		Document:        bytes.NewReader(document),
		SizeBytes:       expectedInputBytes,
		ContentType:     expectedContentType,
		SubmitStartedAt: time.Now().UTC(),
	})

	require.ErrorIs(t, err, storageError)
	assert.Nil(t, ticket)

	harness.jobs.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
	harness.queue.AssertNotCalled(t, "Enqueue", mock.Anything, mock.Anything)
	harness.assertExpectations(t)
}

func TestSubmitTicketCreateError(t *testing.T) {
	harness := newSubmitTestHarness(t)
	document := []byte("my pdf data")
	expectedContentType := config.ContentTypePDF
	expectedInputBytes := int64(len(document))
	createError := errors.New("error creating ticket")

	harness.storage.
		On("Write", submitContextHasTicketID(), mock.Anything, harness.config.documentPath).
		Return(nil).
		Once()
	harness.metrics.
		On("RecordStorageUpload", submitContextHasTicketID(), matchesDocumentStorageUpload(metrics.StorageUploadSuccess)).
		Once()
	harness.jobs.
		On("Create", submitContextHasTicketID(), mock.AnythingOfType("*job.Ticket")).
		Return(createError).
		Once()
	harness.metrics.
		On(
			"RecordSubmission",
			submitContextHasTicketID(),
			metrics.SubmissionObservation{
				Outcome:     metrics.SubmissionFailed,
				Reason:      new(metrics.SubmissionReasonTicketCreationFailed),
				ContentType: &expectedContentType,
				InputBytes:  &expectedInputBytes,
			},
		).
		Once()

	ticket, err := harness.submitter.Submit(t.Context(), SubmitInput{
		Document:        bytes.NewReader(document),
		SizeBytes:       expectedInputBytes,
		ContentType:     expectedContentType,
		SubmitStartedAt: time.Now().UTC(),
	})

	require.ErrorIs(t, err, createError)
	assert.Nil(t, ticket)

	harness.queue.AssertNotCalled(t, "Enqueue", mock.Anything, mock.Anything)
	harness.assertExpectations(t)
}

func TestSubmitEnqueueError(t *testing.T) {
	harness := newSubmitTestHarness(t)
	document := []byte("my pdf data")
	expectedContentType := config.ContentTypePDF
	expectedInputBytes := int64(len(document))
	enqueueError := errors.New("enqueue error")

	harness.storage.
		On("Write", submitContextHasTicketID(), mock.Anything, harness.config.documentPath).
		Return(nil).
		Once()
	harness.metrics.
		On("RecordStorageUpload", submitContextHasTicketID(), matchesDocumentStorageUpload(metrics.StorageUploadSuccess)).
		Once()

	var createdTicket *job.Ticket
	harness.jobs.
		On("Create", submitContextHasTicketID(), mock.AnythingOfType("*job.Ticket")).
		Run(func(args mock.Arguments) {
			createdTicket = args.Get(1).(*job.Ticket)
		}).
		Return(nil).
		Once()
	harness.queue.
		On("Enqueue", submitContextHasTicketID(), mock.AnythingOfType("*job.Ticket")).
		Return(enqueueError).
		Once()
	harness.jobs.
		On(
			"MarkFailed",
			submitContextHasTicketID(),
			mock.AnythingOfType("uuid.UUID"),
			job.EnqueueFailed,
			"enqueue failed. err: enqueue error",
		).
		Run(func(args mock.Arguments) {
			assert.Equal(t, createdTicket.ID, args.Get(1).(uuid.UUID))
		}).
		Return(true, nil).
		Once()

	harness.metrics.
		On(
			"RecordSubmission",
			submitContextHasTicketID(),
			metrics.SubmissionObservation{
				Outcome:     metrics.SubmissionFailed,
				Reason:      new(metrics.SubmissionReasonQueueEnqueueFailed),
				ContentType: &expectedContentType,
				InputBytes:  &expectedInputBytes,
			},
		).
		Once()
	harness.metrics.
		On(
			"RecordJobTerminal",
			submitContextHasTicketID(),
			metrics.TerminalObservation{
				Outcome: metrics.TerminalFailed,
				Reason:  new(metrics.TerminalReasonEnqueueFailed),
			},
		).
		Once()
	harness.metrics.
		On("RecordJobEndToEnd", submitContextHasTicketID(), matchesFailedJobEnd()).
		Once()

	ticket, err := harness.submitter.Submit(t.Context(), SubmitInput{
		Document:        bytes.NewReader(document),
		SizeBytes:       expectedInputBytes,
		ContentType:     expectedContentType,
		SubmitStartedAt: time.Now().UTC(),
	})

	require.ErrorIs(t, err, enqueueError)
	assert.Nil(t, ticket)

	harness.assertExpectations(t)
}

type submitTestHarness struct {
	config    *submitTestConfig
	storage   *submitStorageMock
	metrics   *submitMetricsMock
	jobs      *submitTicketRepositoryMock
	queue     *submitQueueMock
	submitter SubmitService
}

func newSubmitTestHarness(t *testing.T) *submitTestHarness {
	t.Helper()

	configuration := &submitTestConfig{
		t:                      t,
		bucket:                 "bucket",
		bucketPath:             "path",
		documentPath:           "s3://document/path.pdf",
		expectedFileType:       "pdf",
		notificationEmailLimit: 5,
	}
	storageMock := new(submitStorageMock)
	metricsMock := new(submitMetricsMock)
	ticketRepositoryMock := new(submitTicketRepositoryMock)
	queueMock := new(submitQueueMock)

	return &submitTestHarness{
		config:  configuration,
		storage: storageMock,
		metrics: metricsMock,
		jobs:    ticketRepositoryMock,
		queue:   queueMock,
		submitter: NewSubmitService(
			configuration,
			&submitLogger{},
			testutils.NewMockTracer(),
			storageMock,
			metricsMock,
			ticketRepositoryMock,
			queueMock,
		),
	}
}

func (h *submitTestHarness) assertExpectations(t *testing.T) {
	t.Helper()

	h.storage.AssertExpectations(t)
	h.metrics.AssertExpectations(t)
	h.jobs.AssertExpectations(t)
	h.queue.AssertExpectations(t)
}

type submitTestConfig struct {
	config.Config
	t                      *testing.T
	bucket                 string
	bucketPath             string
	documentPath           string
	expectedFileType       string
	receivedFileType       string
	notificationEmailLimit int
}

func (c *submitTestConfig) GetBucket() string {
	return c.bucket
}

func (c *submitTestConfig) GetBucketPath() string {
	return c.bucketPath
}

func (c *submitTestConfig) GetDocumentPath(bucket, bucketPath, jobID, fileType string) string {
	assert.Equal(c.t, c.bucket, bucket)
	assert.Equal(c.t, c.bucketPath, bucketPath)
	assert.NotEmpty(c.t, jobID)
	assert.Equal(c.t, c.expectedFileType, fileType)
	c.receivedFileType = fileType
	return c.documentPath
}

func (c *submitTestConfig) GetNotificationEmailLimit() int {
	return c.notificationEmailLimit
}

var (
	_ config.Config            = (*submitTestConfig)(nil)
	_ storage.Storage          = (*submitStorageMock)(nil)
	_ telemetry.ServiceMetrics = (*submitMetricsMock)(nil)
	_ job.TicketRepository     = (*submitTicketRepositoryMock)(nil)
	_ queue.BatchQueue         = (*submitQueueMock)(nil)
	_ logging.Logger           = (*submitLogger)(nil)
)

type submitStorageMock struct {
	mock.Mock
}

func (m *submitStorageMock) Write(ctx context.Context, reader io.Reader, output string) error {
	return m.Called(ctx, reader, output).Error(0)
}

func (m *submitStorageMock) Read(ctx context.Context, input string) (io.ReadCloser, error) {
	args := m.Called(ctx, input)
	if reader := args.Get(0); reader != nil {
		return reader.(io.ReadCloser), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *submitStorageMock) GetLink(ctx context.Context, input string) (string, time.Time, error) {
	args := m.Called(ctx, input)
	return args.String(0), args.Get(1).(time.Time), args.Error(2)
}

type submitMetricsMock struct {
	mock.Mock
}

func (m *submitMetricsMock) RecordSubmission(ctx context.Context, observation metrics.SubmissionObservation) {
	m.Called(ctx, observation)
}

func (m *submitMetricsMock) RecordStorageUpload(ctx context.Context, observation metrics.StorageUploadObservation) {
	m.Called(ctx, observation)
}

func (m *submitMetricsMock) RecordResponseWriteError(ctx context.Context, observation metrics.ResponseObservation) {
	m.Called(ctx, observation)
}

func (m *submitMetricsMock) RecordJobTerminal(ctx context.Context, observation metrics.TerminalObservation) {
	m.Called(ctx, observation)
}

func (m *submitMetricsMock) RecordJobEndToEnd(ctx context.Context, observation metrics.JobEndObservation) {
	m.Called(ctx, observation)
}

type submitTicketRepositoryMock struct {
	mock.Mock
}

func (m *submitTicketRepositoryMock) Create(ctx context.Context, ticket *job.Ticket) error {
	return m.Called(ctx, ticket).Error(0)
}

func (m *submitTicketRepositoryMock) Get(ctx context.Context, id uuid.UUID) (*job.Ticket, error) {
	args := m.Called(ctx, id)
	if ticket := args.Get(0); ticket != nil {
		return ticket.(*job.Ticket), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *submitTicketRepositoryMock) Update(ctx context.Context, ticket *job.Ticket) error {
	return m.Called(ctx, ticket).Error(0)
}

func (m *submitTicketRepositoryMock) MarkFailed(ctx context.Context, id uuid.UUID, reason job.TicketStatusReason, detail string) (bool, error) {
	args := m.Called(ctx, id, reason, detail)
	return args.Bool(0), args.Error(1)
}

type submitQueueMock struct {
	mock.Mock
}

func (m *submitQueueMock) Ready(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

func (m *submitQueueMock) Enqueue(ctx context.Context, ticket *job.Ticket) error {
	return m.Called(ctx, ticket).Error(0)
}

func (m *submitQueueMock) Dequeue(ctx context.Context) (*queue.DequeueResult, error) {
	args := m.Called(ctx)
	if result := args.Get(0); result != nil {
		return result.(*queue.DequeueResult), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *submitQueueMock) DeleteMessage(ctx context.Context, queueReference string) error {
	return m.Called(ctx, queueReference).Error(0)
}

type submitLogger struct{}

func (*submitLogger) Debug(context.Context, string, ...any) {}

func (*submitLogger) Info(context.Context, string, ...any) {}

func (*submitLogger) Warn(context.Context, string, ...any) {}

func (*submitLogger) Error(context.Context, string, ...any) {}

func (l *submitLogger) WithContext(context.Context) logging.Logger { return l }

func submitContextHasTicketID() any {
	return mock.MatchedBy(func(ctx context.Context) bool {
		_, ok := logging.GetTicketID(ctx)
		return ok
	})
}

func matchesDocumentStorageUpload(outcome metrics.StorageUploadOutcome) any {
	return mock.MatchedBy(func(observation metrics.StorageUploadObservation) bool {
		return observation.Duration >= 0 &&
			observation.Purpose == metrics.StorageUploadPurposeDocument &&
			observation.Outcome == outcome
	})
}

func matchesFailedJobEnd() any {
	return mock.MatchedBy(func(observation metrics.JobEndObservation) bool {
		return observation.Duration >= 0 &&
			observation.Outcome == metrics.JobEndOutcomeFailed &&
			observation.Reason != nil &&
			*observation.Reason == metrics.TerminalReasonEnqueueFailed
	})
}
