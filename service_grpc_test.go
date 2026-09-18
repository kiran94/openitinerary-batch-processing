package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/service"
	telemetrymetrics "github.com/kiran94/openitinerary-batch-processing/internal/telemetry/metrics"
	batchv1 "github.com/kiran94/openitinerary-batch-processing/proto/gen/go/openitinerary/batch/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type grpcServiceHarness struct {
	cfg           *MockConfig
	logger        *MockLogger
	metrics       *MockServiceMetrics
	submitService *MockSubmitService
	statusService *MockStatusService
	resultService *MockResultService

	grpcClient  batchv1.OpenItineraryBatchProcessingServiceClient
	grpcService *OpenItineraryBatchProcessingGrpcService
}

const grpcTestBufferSize = 1024 * 1024

func createHarness(t *testing.T) grpcServiceHarness {
	t.Helper()

	harness := grpcServiceHarness{
		cfg:           &MockConfig{},
		logger:        &MockLogger{},
		metrics:       &MockServiceMetrics{},
		submitService: &MockSubmitService{},
		statusService: &MockStatusService{},
		resultService: &MockResultService{},
	}

	harness.grpcService = &OpenItineraryBatchProcessingGrpcService{
		batchv1.UnimplementedOpenItineraryBatchProcessingServiceServer{},
		harness.cfg,
		harness.logger,
		harness.metrics,
		harness.submitService,
		harness.statusService,
		harness.resultService,
	}

	listener := bufconn.Listen(grpcTestBufferSize)
	server := grpc.NewServer()

	batchv1.RegisterOpenItineraryBatchProcessingServiceServer(
		server,
		harness.grpcService,
	)

	go func() {
		_ = server.Serve(listener)
	}()

	connection, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(
			func(ctx context.Context, _ string) (net.Conn, error) {
				return listener.DialContext(ctx)
			},
		),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	harness.grpcClient = batchv1.NewOpenItineraryBatchProcessingServiceClient(
		connection,
	)

	t.Cleanup(func() {
		require.NoError(t, connection.Close())
		server.Stop()
		require.NoError(t, listener.Close())

		harness.cfg.AssertExpectations(t)
		harness.metrics.AssertExpectations(t)
		harness.submitService.AssertExpectations(t)
		harness.statusService.AssertExpectations(t)
		harness.resultService.AssertExpectations(t)
	})

	return harness
}

func TestResultLink_TicketIdInvalid(t *testing.T) {
	tests := []struct {
		ticketID string
	}{
		{ticketID: ""},
		{ticketID: "not-a-uuid"},
	}

	for _, tt := range tests {
		t.Run(tt.ticketID, func(t *testing.T) {
			ctx := t.Context()

			harness := createHarness(t)
			_, err := harness.grpcClient.ResultLink(ctx, &batchv1.ResultLinkRequest{
				TicketId: tt.ticketID,
			})

			stat, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, codes.InvalidArgument, stat.Code())
		})
	}
}

func TestResultLink_ErrorGettingResultLink(t *testing.T) {
	const ticketID = "6512026f-1eb6-47c7-9e9b-cf4cf707e9fe"
	tests := []struct {
		name               string
		getResultLinkError error
		expectedStatusCode codes.Code
	}{
		{name: "job_not_complete", getResultLinkError: service.ErrJobNotComplete, expectedStatusCode: codes.NotFound},
		{name: "job_not_found", getResultLinkError: job.ErrRecordNotFound, expectedStatusCode: codes.NotFound},
		{name: "other_error", getResultLinkError: errors.New("generic error"), expectedStatusCode: codes.Internal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := createHarness(t)

			harness.resultService.On("GetResultLink", mock.Anything, uuid.MustParse(ticketID)).Return(nil, tt.getResultLinkError).Once()
			_, err := harness.grpcClient.ResultLink(t.Context(), &batchv1.ResultLinkRequest{
				TicketId: ticketID,
			})

			stat, ok := status.FromError(err)
			require.True(t, ok)

			assert.Equal(t, tt.expectedStatusCode, stat.Code())
		})
	}
}

func TestResultLink_DeadlineExceeded(t *testing.T) {
	const ticketID = "6512026f-1eb6-47c7-9e9b-cf4cf707e9fe"

	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()

	harness := createHarness(t)
	harness.resultService.
		On("GetResultLink", mock.Anything, uuid.MustParse(ticketID)).
		Return(nil, context.DeadlineExceeded).
		Once()

	response, err := harness.grpcService.ResultLink(ctx, &batchv1.ResultLinkRequest{TicketId: ticketID})

	require.Error(t, err)
	assert.Nil(t, response)
	assert.Equal(t, codes.DeadlineExceeded, status.Code(err))
}

func TestResultLink_Success(t *testing.T) {
	const (
		ticketID = "6512026f-1eb6-47c7-9e9b-cf4cf707e9fe"
		link     = "s3://my-bucket/result.json"
	)

	harness := createHarness(t)

	expiration := time.Now().Add(time.Hour * 1)
	getResultLinkServiceResponse := &service.ResultOutput{
		Link:    link,
		Expires: expiration,
	}

	harness.resultService.On("GetResultLink", mock.Anything, uuid.MustParse(ticketID)).Return(getResultLinkServiceResponse, nil).Once()
	response, err := harness.grpcClient.ResultLink(t.Context(), &batchv1.ResultLinkRequest{
		TicketId: ticketID,
	})

	stat, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.OK, stat.Code())

	assert.Equal(t, link, response.Link)
	assert.Equal(t, expiration.UTC(), response.Expires.AsTime().UTC())
}

func TestStatus_InvalidTicket(t *testing.T) {
	tests := []struct {
		ticketID string
	}{
		{ticketID: ""},
		{ticketID: "not-a-uuid"},
	}

	for _, tt := range tests {
		t.Run(tt.ticketID, func(t *testing.T) {
			ctx := t.Context()

			harness := createHarness(t)
			_, err := harness.grpcClient.Status(ctx, &batchv1.StatusRequest{
				TicketId: tt.ticketID,
			})

			stat, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, codes.InvalidArgument, stat.Code())
		})
	}
}

func TestStatus_GetStatusError(t *testing.T) {
	const ticketID = "6512026f-1eb6-47c7-9e9b-cf4cf707e9fe"
	tests := []struct {
		name               string
		getResultLinkError error
		expectedStatusCode codes.Code
	}{
		{name: "job_not_found", getResultLinkError: job.ErrRecordNotFound, expectedStatusCode: codes.NotFound},
		{name: "other_error", getResultLinkError: errors.New("generic error"), expectedStatusCode: codes.Internal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := createHarness(t)

			harness.statusService.On("GetStatus", mock.Anything, uuid.MustParse(ticketID)).Return(nil, tt.getResultLinkError).Once()
			_, err := harness.grpcClient.Status(t.Context(), &batchv1.StatusRequest{
				TicketId: ticketID,
			})

			stat, ok := status.FromError(err)
			require.True(t, ok)

			assert.Equal(t, tt.expectedStatusCode, stat.Code())
		})
	}
}

func TestStatus_ContextCanceled(t *testing.T) {
	const ticketID = "6512026f-1eb6-47c7-9e9b-cf4cf707e9fe"

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	harness := createHarness(t)
	harness.statusService.
		On("GetStatus", mock.Anything, uuid.MustParse(ticketID)).
		Return(nil, context.Canceled).
		Once()

	response, err := harness.grpcService.Status(ctx, &batchv1.StatusRequest{TicketId: ticketID})

	require.Error(t, err)
	assert.Nil(t, response)
	assert.Equal(t, codes.Canceled, status.Code(err))
}

func TestStatus_Success(t *testing.T) {
	const (
		ticketID = "6512026f-1eb6-47c7-9e9b-cf4cf707e9fe"
		link     = "s3://my-bucket/result.json"
	)

	ticket := job.Ticket{ID: uuid.MustParse(ticketID)}

	harness := createHarness(t)
	harness.statusService.On("GetStatus", mock.Anything, uuid.MustParse(ticketID)).Return(&ticket, nil).Once()
	response, err := harness.grpcClient.Status(t.Context(), &batchv1.StatusRequest{
		TicketId: ticketID,
	})

	stat, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.OK, stat.Code())

	assert.Equal(t, ticket.ID.String(), response.Ticket.Id)
}

func TestSubmit_MetadataFetchFailed(t *testing.T) {
	tests := []struct {
		name           string
		metadata       *batchv1.SubmitMetadata
		expectedStatus codes.Code
	}{
		{name: "metadata_nil", metadata: nil, expectedStatus: codes.InvalidArgument},
		{name: "unspecified_content_type", metadata: &batchv1.SubmitMetadata{ContentType: batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_UNSPECIFIED}, expectedStatus: codes.InvalidArgument},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			harness := createHarness(t)

			stream, err := harness.grpcClient.Submit(ctx)
			require.NoError(t, err)

			if tt.metadata == nil {
				require.NoError(t, stream.Send(&batchv1.SubmitRequest{Frame: nil}))
			} else {
				require.NoError(t, stream.Send(&batchv1.SubmitRequest{
					Frame: &batchv1.SubmitRequest_Metadata{Metadata: tt.metadata},
				}))
			}
			_, err = stream.CloseAndRecv()

			require.Error(t, err)
			stat, ok := status.FromError(err)
			require.True(t, ok)

			assert.Equal(t, tt.expectedStatus, stat.Code())
		})
	}
}

func TestSubmit_EmptyStream(t *testing.T) {
	harness := createHarness(t)

	stream, err := harness.grpcClient.Submit(t.Context())
	require.NoError(t, err)

	response, rpcErr := stream.CloseAndRecv()

	require.Error(t, rpcErr)
	assert.Nil(t, response)
	assert.Equal(t, codes.InvalidArgument, status.Code(rpcErr))
}

func TestSubmit_InvalidDocumentFrame(t *testing.T) {
	tests := []struct {
		name  string
		frame *batchv1.SubmitRequest
	}{
		{
			name: "second_metadata",
			frame: &batchv1.SubmitRequest{
				Frame: &batchv1.SubmitRequest_Metadata{
					Metadata: &batchv1.SubmitMetadata{
						ContentType: batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_PDF,
					},
				},
			},
		},
		{
			name:  "frame_not_set",
			frame: &batchv1.SubmitRequest{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			documentErrChan := make(chan error, 1)
			harness := createHarness(t)
			harness.cfg.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
			harness.submitService.
				On("Submit", mock.Anything, mock.Anything).
				Run(func(args mock.Arguments) {
					submitInput := args.Get(1).(service.SubmitInput)
					_, err := io.ReadAll(submitInput.Document)
					documentErrChan <- err
				}).
				Return(nil, service.ErrInvalidDocumentInput).
				Once()

			stream, err := harness.grpcClient.Submit(t.Context())
			require.NoError(t, err)

			require.NoError(t, stream.Send(&batchv1.SubmitRequest{
				Frame: &batchv1.SubmitRequest_Metadata{
					Metadata: &batchv1.SubmitMetadata{
						ContentType: batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_PDF,
					},
				},
			}))
			require.NoError(t, stream.Send(tt.frame))

			response, rpcErr := stream.CloseAndRecv()

			require.Error(t, rpcErr)
			assert.Nil(t, response)
			assert.Equal(t, codes.InvalidArgument, status.Code(rpcErr))

			documentErr := <-documentErrChan
			require.ErrorIs(t, documentErr, service.ErrInvalidDocumentInput)
		})
	}
}

func TestSubmit_ServiceError(t *testing.T) {
	tests := []struct {
		name               string
		submitError        error
		expectedStatusCode codes.Code
	}{
		{name: "invalid_notification_emails", submitError: service.ErrInvalidNotificationEmails{}, expectedStatusCode: codes.InvalidArgument},
		{name: "document_too_large", submitError: service.ErrDocumentTooLarge, expectedStatusCode: codes.ResourceExhausted},
		{name: "internal_error", submitError: errors.New("submit failed"), expectedStatusCode: codes.Internal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := createHarness(t)
			harness.cfg.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
			harness.submitService.
				On("Submit", mock.Anything, mock.Anything).
				Return(nil, tt.submitError).
				Once()

			stream, err := harness.grpcClient.Submit(t.Context())
			require.NoError(t, err)

			require.NoError(t, stream.Send(&batchv1.SubmitRequest{
				Frame: &batchv1.SubmitRequest_Metadata{
					Metadata: &batchv1.SubmitMetadata{
						ContentType: batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_PDF,
					},
				},
			}))

			response, rpcErr := stream.CloseAndRecv()

			require.Error(t, rpcErr)
			assert.Nil(t, response)
			assert.Equal(t, tt.expectedStatusCode, status.Code(rpcErr))
		})
	}
}

func TestSubmit_DocumentTooLarge(t *testing.T) {
	documentErrChan := make(chan error, 1)

	harness := createHarness(t)
	harness.cfg.On("GetMaximumAllowedRequestSizeMb").Return(int64(3)).Once()
	harness.submitService.
		On("Submit", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			input := args.Get(1).(service.SubmitInput)
			_, err := io.ReadAll(input.Document)
			documentErrChan <- err
		}).
		Return(nil, service.ErrDocumentTooLarge).
		Once()

	stream, err := harness.grpcClient.Submit(t.Context())
	require.NoError(t, err)

	require.NoError(t, stream.Send(&batchv1.SubmitRequest{
		Frame: &batchv1.SubmitRequest_Metadata{
			Metadata: &batchv1.SubmitMetadata{
				ContentType: batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_PDF,
			},
		},
	}))
	require.NoError(t, stream.Send(&batchv1.SubmitRequest{
		Frame: &batchv1.SubmitRequest_Chunk{Chunk: []byte("four")},
	}))

	response, rpcErr := stream.CloseAndRecv()

	require.Error(t, rpcErr)
	assert.Nil(t, response)
	assert.Equal(t, codes.ResourceExhausted, status.Code(rpcErr))

	documentErr := <-documentErrChan
	require.ErrorIs(t, documentErr, service.ErrDocumentTooLarge)
}

func TestSubmit_Success(t *testing.T) {
	documentChunks := [][]byte{
		[]byte("first "),
		[]byte("second"),
	}
	expectedDocument := []byte("first second")
	expectedEmails := []string{"hello@email.com", "second@email.com"}
	expectedContentType := config.ContentTypePDF
	expectedInputBytes := int64(len(expectedDocument))
	expectedServiceType := telemetrymetrics.SubmissionServiceType(telemetrymetrics.SubmissionServiceTypeGRPC)

	ticket := job.NewTicket()
	ticket.ContentType = string(expectedContentType)

	type capturedSubmission struct {
		document []byte
		input    service.SubmitInput
		err      error
	}
	capturedSubmissionChan := make(chan capturedSubmission, 1)

	harness := createHarness(t)
	harness.cfg.On("GetMaximumAllowedRequestSizeMb").Return(int64(10 << 20)).Once()
	harness.submitService.
		On("Submit", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			input := args.Get(1).(service.SubmitInput)
			document, err := io.ReadAll(input.Document)
			capturedSubmissionChan <- capturedSubmission{
				document: document,
				input:    input,
				err:      err,
			}
		}).
		Return(ticket, nil).
		Once()
	harness.metrics.
		On(
			"RecordSubmission",
			contextHasTicketIDValue(ticket.ID),
			mock.MatchedBy(func(observation telemetrymetrics.SubmissionObservation) bool {
				return observation.Outcome == telemetrymetrics.SubmissionAccepted &&
					observation.Enqueued &&
					observation.InputBytes != nil &&
					*observation.InputBytes == expectedInputBytes &&
					observation.ContentType != nil &&
					*observation.ContentType == expectedContentType &&
					observation.ServiceType == expectedServiceType
			}),
		).
		Once()

	stream, err := harness.grpcClient.Submit(t.Context())
	require.NoError(t, err)

	require.NoError(t, stream.Send(&batchv1.SubmitRequest{
		Frame: &batchv1.SubmitRequest_Metadata{
			Metadata: &batchv1.SubmitMetadata{
				ContentType:        batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_PDF,
				NotificationEmails: expectedEmails,
			},
		},
	}))
	for _, chunk := range documentChunks {
		require.NoError(t, stream.Send(&batchv1.SubmitRequest{
			Frame: &batchv1.SubmitRequest_Chunk{Chunk: chunk},
		}))
	}

	response, err := stream.CloseAndRecv()

	require.NoError(t, err)
	require.NotNil(t, response)
	require.NotNil(t, response.Ticket)

	assert.Equal(t, ticket.ID.String(), response.Ticket.Id)
	assert.Equal(t, batchv1.DocumentContentType_DOCUMENT_CONTENT_TYPE_PDF, response.Ticket.ContentType)
	assert.Equal(t, batchv1.TicketStatus_TICKET_STATUS_QUEUED, response.Ticket.TicketStatus)

	captured := <-capturedSubmissionChan
	require.NoError(t, captured.err)
	assert.Equal(t, expectedDocument, captured.document)
	assert.Equal(t, expectedContentType, captured.input.ContentType)
	assert.Equal(t, expectedEmails, captured.input.NotificationEmails)
	assert.Equal(t, expectedServiceType, captured.input.ServiceType)
	assert.False(t, captured.input.SubmitStartedAt.IsZero())
	assert.Equal(t, time.UTC, captured.input.SubmitStartedAt.Location())
}

func TestCountingReaderDocumentTooLarge(t *testing.T) {
	reader := &CountingReader{
		reader:  bytes.NewReader([]byte("12345678901")),
		maximum: 10,
	}

	data, err := io.ReadAll(reader)

	require.ErrorIs(t, err, service.ErrDocumentTooLarge)
	assert.Len(t, data, 11)
	assert.Equal(t, int64(11), reader.SizeBytes())
}

func TestShutdownGrpcServer_NormalShutdown(t *testing.T) {
	logger := &MockLogger{}
	server := grpc.NewServer()

	shutdownGrpcServer(server, logger)
}
