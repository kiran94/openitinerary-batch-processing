package interceptors

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestUnary_DeadlineExceeded(t *testing.T) {
	ctx := context.Background()

	mockConfig := mockConfig{}
	mockConfig.On("GetGRPCUploadTimeout").Return(time.Second * 0).Once()

	interceptor := TimeoutGrpcInterceptor{Config: &mockConfig}
	_, err := interceptor.Unary(ctx, nil, nil, func(ctx context.Context, req any) (any, error) {

		select {
		case <-ctx.Done():
			// execution should reach here
			require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
		default:
			assert.Fail(t, "work was cancelled but still attempted to run")
		}

		return nil, nil
	})

	require.NoError(t, err)
	mockConfig.AssertExpectations(t)
}

func TestUnary_ParentCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	mockConfig := mockConfig{}
	mockConfig.On("GetGRPCUploadTimeout").Return(time.Hour).Once()

	interceptor := TimeoutGrpcInterceptor{Config: &mockConfig}
	_, err := interceptor.Unary(ctx, nil, nil, func(ctx context.Context, req any) (any, error) {
		<-ctx.Done()

		require.ErrorIs(t, ctx.Err(), context.Canceled)

		return nil, ctx.Err()
	})

	require.ErrorIs(t, err, context.Canceled)
	mockConfig.AssertExpectations(t)
}

func TestUnary_NotCancelled(t *testing.T) {
	ctx := context.Background()

	mockConfig := mockConfig{}
	mockConfig.On("GetGRPCUploadTimeout").Return(time.Hour * 1).Once() // very large timeout

	interceptor := TimeoutGrpcInterceptor{Config: &mockConfig}
	_, err := interceptor.Unary(ctx, nil, nil, func(ctx context.Context, req any) (any, error) {
		select {
		case <-ctx.Done():
			assert.Fail(t, "should not have reached cancellation")
		default:
			// Since the work will never be cancelled then this branch
			// should execute
		}

		return nil, nil
	})

	require.NoError(t, err)
	mockConfig.AssertExpectations(t)
}

func TestStreaming_DeadlineExceeded(t *testing.T) {
	cfg := new(mockConfig)
	cfg.On("GetGRPCUploadTimeout").Return(10 * time.Millisecond).Once()

	stream := &blockingServerStream{
		ctx:     t.Context(),
		release: make(chan struct{}),
	}
	t.Cleanup(func() {
		close(stream.release)
	})

	interceptor := TimeoutGrpcInterceptor{Config: cfg}
	err := interceptor.Streaming(nil, stream, nil, func(_ any, stream grpc.ServerStream) error {
		return stream.RecvMsg(new([]byte))
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	cfg.AssertExpectations(t)
}

func TestStreaming_ParentCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cfg := new(mockConfig)
	cfg.On("GetGRPCUploadTimeout").Return(time.Hour).Once()

	stream := &blockingServerStream{
		ctx:     ctx,
		release: make(chan struct{}),
	}
	t.Cleanup(func() {
		close(stream.release)
	})

	interceptor := TimeoutGrpcInterceptor{Config: cfg}
	err := interceptor.Streaming(nil, stream, nil, func(_ any, stream grpc.ServerStream) error {
		return stream.RecvMsg(new([]byte))
	})

	require.ErrorIs(t, err, context.Canceled)
	cfg.AssertExpectations(t)
}

func TestStreaming_WorkDone(t *testing.T) {
	cfg := new(mockConfig)
	cfg.On("GetGRPCUploadTimeout").Return(10 * time.Millisecond).Once()

	stream := &blockingServerStream{
		ctx:     t.Context(),
		release: make(chan struct{}),
	}

	// Mark Completion
	close(stream.release)

	interceptor := TimeoutGrpcInterceptor{Config: cfg}
	err := interceptor.Streaming(nil, stream, nil, func(_ any, stream grpc.ServerStream) error {
		return stream.RecvMsg(new([]byte))
	})

	require.ErrorIs(t, err, io.EOF)
	cfg.AssertExpectations(t)
}

// MOCKS

type blockingServerStream struct {
	grpc.ServerStream
	ctx     context.Context
	release chan struct{}
}

func (b *blockingServerStream) Context() context.Context {
	return b.ctx
}

func (b *blockingServerStream) RecvMsg(m any) error {
	<-b.release
	return io.EOF
}

type mockConfig struct {
	mock.Mock
}

func (m *mockConfig) GetEnvironment() string {
	return m.Called().String(0)
}

func (m *mockConfig) GetPort() string {
	return m.Called().String(0)
}

func (m *mockConfig) GetLoggingLevel() slog.Level {
	return m.Called().Get(0).(slog.Level)
}

func (m *mockConfig) GetDatabaseConnection() string {
	return m.Called().String(0)
}

func (m *mockConfig) GetServerReadTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *mockConfig) GetServerWriteTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *mockConfig) GetServerIdleTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *mockConfig) GetGrpcServerIdleTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *mockConfig) GetGrpcServerMaxConnectionAge() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *mockConfig) GetGRPCUploadTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *mockConfig) GetMaximumAllowedRequestSizeMb() int64 {
	return m.Called().Get(0).(int64)
}

func (m *mockConfig) GetMaximumBufferMultipartForm() int64 {
	return m.Called().Get(0).(int64)
}

func (m *mockConfig) GetMaximumAdapterResponseSizeBytes() int64 {
	return m.Called().Get(0).(int64)
}

func (m *mockConfig) GetBucket() string {
	return m.Called().String(0)
}

func (m *mockConfig) GetBucketPath() string {
	return m.Called().String(0)
}

func (m *mockConfig) GetS3UploadConcurrency() int {
	return m.Called().Int(0)
}

func (m *mockConfig) GetS3PresignedExpirationSeconds() int {
	return m.Called().Int(0)
}

func (m *mockConfig) GetS3UsePathStyle() bool {
	return m.Called().Bool(0)
}

func (m *mockConfig) GetDocumentPath(bucket, bucketPath, jobID, fileType string) string {
	return m.Called(bucket, bucketPath, jobID, fileType).String(0)
}

func (m *mockConfig) GetOutputPath(bucket, bucketPath, jobID string) string {
	return m.Called(bucket, bucketPath, jobID).String(0)
}

func (m *mockConfig) GetQueueURL() string {
	return m.Called().String()
}

func (m *mockConfig) GetQueuePollingTime() int32 {
	return m.Called().Get(0).(int32)
}

func (m *mockConfig) GetSESConfigurationSetName() string {
	return m.Called().String(0)
}

func (m *mockConfig) GetSESAddressFrom() string {
	return m.Called().String(0)
}

func (m *mockConfig) GetNotificationEmailLimit() int {
	return m.Called().Int(0)
}

func (m *mockConfig) GetWorkerTimeoutSeconds() int {
	return m.Called().Int(0)
}

func (m *mockConfig) GetOtelEnabled() bool {
	return m.Called().Bool(0)
}

func (m *mockConfig) GetOtelServiceName() string {
	return m.Called().String(0)
}

func (m *mockConfig) GetOtelWorkerName() string {
	return m.Called().String(0)
}

func (m *mockConfig) GetOtelGrpcName() string {
	return m.Called().String(0)
}

func (m *mockConfig) GetOtelServiceVersion() string {
	return m.Called().String(0)
}

func (m *mockConfig) GetServiceReadinessPollingInterval() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *mockConfig) GetServiceReadinessPollingTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}
