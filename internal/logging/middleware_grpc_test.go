package logging

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestRequestIDUnaryInterceptor_MetadataNotFound(t *testing.T) {
	ctx := context.Background()
	// Not adding any metadata

	_, err := RequestIDUnaryInterceptor(ctx, nil, nil, nil)

	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, errCouldNotGetMetadata.Error(), status.Convert(err).Message())
}

func TestRequestIDUnaryInterceptor_RequestIDNotExists(t *testing.T) {
	var responseRequestID string
	transportStreamMock := new(mockServerTransportStream)
	transportStreamMock.On("SetHeader", mock.Anything).Run(func(args mock.Arguments) {
		responseRequestID = extractRequestIDFromMetadata(t, args, "")
	}).Return(nil)

	ctx := grpc.NewContextWithServerTransportStream(context.Background(), transportStreamMock)
	ctx = metadata.NewIncomingContext(ctx, metadata.MD{
		"not_request_id": []string{"hello"},
	})

	handler := func(ctx context.Context, req any) (any, error) {
		rid, ok := GetRequestID(ctx)
		require.True(t, ok)
		require.NotEqual(t, uuid.Nil.String(), rid)
		assert.Equal(t, responseRequestID, rid)

		return nil, nil
	}

	_, err := RequestIDUnaryInterceptor(ctx, nil, nil, handler)
	assert.NoError(t, err)
}

func TestRequestIDUnaryInterceptor_RequestIDExists(t *testing.T) {
	requestID := "b1e9ce0b-f95f-410b-9288-98dffe2b0956"

	transportStreamMock := new(mockServerTransportStream)
	transportStreamMock.On("SetHeader", mock.Anything).Run(func(args mock.Arguments) {
		extractRequestIDFromMetadata(t, args, requestID)
	}).Return(nil)

	ctx := grpc.NewContextWithServerTransportStream(context.Background(), transportStreamMock)
	ctx = metadata.NewIncomingContext(ctx, metadata.MD{HeaderRequestID: []string{requestID}})

	handler := func(ctx context.Context, req any) (any, error) {
		rid, ok := GetRequestID(ctx)
		require.True(t, ok)
		assert.Equal(t, requestID, rid)

		return nil, nil
	}

	_, err := RequestIDUnaryInterceptor(ctx, nil, nil, handler)
	assert.NoError(t, err)
}

func TestRequestIDUnaryInterceptor_RequestIDSetButInvalid(t *testing.T) {
	requestID := "not-a-uuid"

	ctx := grpc.NewContextWithServerTransportStream(context.Background(), &mockServerTransportStream{})
	ctx = metadata.NewIncomingContext(ctx, metadata.MD{HeaderRequestID: []string{requestID}})

	_, err := RequestIDUnaryInterceptor(ctx, nil, nil, nil)

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, errUUIDNotValid.Error(), status.Convert(err).Message())
}

func TestRequestIDUnaryInterceptor_MultipleRequestIDs(t *testing.T) {
	ctx := grpc.NewContextWithServerTransportStream(context.Background(), &mockServerTransportStream{})
	ctx = metadata.NewIncomingContext(ctx, metadata.MD{
		HeaderRequestID: []string{uuid.NewString(), uuid.NewString()},
	})

	_, err := RequestIDUnaryInterceptor(ctx, nil, nil, nil)

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, errMultipleRequestIDs.Error(), status.Convert(err).Message())
}

func TestRequestIDUnaryInterceptor_FailedSetHeader(t *testing.T) {
	requestID := "b1e9ce0b-f95f-410b-9288-98dffe2b0956"

	transportStreamMock := new(mockServerTransportStream)
	transportStreamMock.On("SetHeader", mock.Anything).Return(errors.New("error setting header"))

	ctx := grpc.NewContextWithServerTransportStream(context.Background(), transportStreamMock)
	ctx = metadata.NewIncomingContext(ctx, metadata.MD{HeaderRequestID: []string{requestID}})

	_, err := RequestIDUnaryInterceptor(ctx, nil, nil, nil)
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, errFailedSetHeader.Error(), status.Convert(err).Message())
}

func TestRequestIDStreamingInterceptor_MetadataNotFound(t *testing.T) {
	stream := &mockServerStream{ctx: context.Background()}

	err := RequestIDStreamingInterceptor(nil, stream, nil, nil)

	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, errCouldNotGetMetadata.Error(), status.Convert(err).Message())
}

func TestRequestIDStreamingInterceptor_RequestIDNotExists(t *testing.T) {
	var responseRequestID string
	transportStreamMock := new(mockServerTransportStream)
	transportStreamMock.On("SetHeader", mock.Anything).Run(func(args mock.Arguments) {
		responseRequestID = extractRequestIDFromMetadata(t, args, "")
	}).Return(nil)

	ctx := grpc.NewContextWithServerTransportStream(context.Background(), transportStreamMock)
	ctx = metadata.NewIncomingContext(ctx, metadata.MD{
		"not_request_id": []string{"hello"},
	})
	stream := &mockServerStream{ctx: ctx}

	handler := func(srv any, stream grpc.ServerStream) error {
		rid, ok := GetRequestID(stream.Context())
		require.True(t, ok)
		require.NotEqual(t, uuid.Nil.String(), rid)
		assert.Equal(t, responseRequestID, rid)

		return nil
	}

	err := RequestIDStreamingInterceptor(nil, stream, nil, handler)
	require.NoError(t, err)
}

func TestRequestIDStreamingInterceptor_RequestIDExists(t *testing.T) {
	requestID := "b1e9ce0b-f95f-410b-9288-98dffe2b0956"

	transportStreamMock := new(mockServerTransportStream)
	transportStreamMock.On("SetHeader", mock.Anything).Run(func(args mock.Arguments) {
		extractRequestIDFromMetadata(t, args, requestID)
	}).Return(nil)

	ctx := grpc.NewContextWithServerTransportStream(context.Background(), transportStreamMock)
	ctx = metadata.NewIncomingContext(ctx, metadata.MD{HeaderRequestID: []string{requestID}})
	stream := &mockServerStream{ctx: ctx}

	handler := func(srv any, stream grpc.ServerStream) error {
		rid, ok := GetRequestID(stream.Context())
		require.True(t, ok)
		assert.Equal(t, requestID, rid)

		return nil
	}

	err := RequestIDStreamingInterceptor(nil, stream, nil, handler)
	assert.NoError(t, err)
}

func TestRequestIDStreamingInterceptor_RequestIDSetButInvalid(t *testing.T) {
	requestID := "not-a-uuid"

	ctx := grpc.NewContextWithServerTransportStream(context.Background(), &mockServerTransportStream{})
	ctx = metadata.NewIncomingContext(ctx, metadata.MD{HeaderRequestID: []string{requestID}})
	stream := &mockServerStream{ctx: ctx}

	err := RequestIDStreamingInterceptor(nil, stream, nil, nil)

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, errUUIDNotValid.Error(), status.Convert(err).Message())
}

func TestRequestIDStreamingInterceptor_MultipleRequestIDs(t *testing.T) {
	ctx := grpc.NewContextWithServerTransportStream(context.Background(), &mockServerTransportStream{})
	ctx = metadata.NewIncomingContext(ctx, metadata.MD{
		HeaderRequestID: []string{uuid.NewString(), uuid.NewString()},
	})
	stream := &mockServerStream{ctx: ctx}

	err := RequestIDStreamingInterceptor(nil, stream, nil, nil)

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, errMultipleRequestIDs.Error(), status.Convert(err).Message())
}

func TestRequestIDStreamingInterceptor_FailedSetHeader(t *testing.T) {
	requestID := "b1e9ce0b-f95f-410b-9288-98dffe2b0956"

	transportStreamMock := new(mockServerTransportStream)
	transportStreamMock.On("SetHeader", mock.Anything).Return(errors.New("error setting header"))

	ctx := grpc.NewContextWithServerTransportStream(context.Background(), transportStreamMock)
	ctx = metadata.NewIncomingContext(ctx, metadata.MD{HeaderRequestID: []string{requestID}})
	stream := &mockServerStream{ctx: ctx}

	err := RequestIDStreamingInterceptor(nil, stream, nil, nil)
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, errFailedSetHeader.Error(), status.Convert(err).Message())
}

func extractRequestIDFromMetadata(t *testing.T, args mock.Arguments, expectedUUID string) string {
	t.Helper()

	a, ok := args.Get(0).(metadata.MD)
	require.True(t, ok)
	assert.Len(t, a, 1)

	rids := a.Get(HeaderRequestID)
	require.Len(t, rids, 1)

	_, err := uuid.Parse(rids[0])
	require.NoError(t, err)

	if expectedUUID != "" {
		assert.Equal(t, expectedUUID, rids[0])
	}

	return rids[0]
}

// MOCKS

type mockServerTransportStream struct {
	mock.Mock
}

func (m *mockServerTransportStream) Method() string {
	return m.Called().String(0)
}

func (m *mockServerTransportStream) SetHeader(md metadata.MD) error {
	return m.Called(md).Error(0)
}

func (m *mockServerTransportStream) SendHeader(md metadata.MD) error {
	return m.Called(md).Error(0)
}

func (m *mockServerTransportStream) SetTrailer(md metadata.MD) error {
	return m.Called(md).Error(0)
}

type mockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *mockServerStream) Context() context.Context {
	return m.ctx
}
