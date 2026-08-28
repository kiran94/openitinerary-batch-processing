package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewAWSS3Storage(t *testing.T) {
	storage, err := NewAWSS3Storage(t.Context())

	assert.NotNil(t, storage)
	assert.NoError(t, err)
}

func TestAWSS3StorageWriteInvalidS3Address(t *testing.T) {

	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		{name: "not_s3_path", input: "not_s3_path", expectedErr: "should be an s3 address"},
		{name: "empty_bucket", input: "s3:///my_path", expectedErr: "s3 bucket cannot be empty"},
		{name: "empty_path", input: "s3://bucket", expectedErr: "s3 key cannot be empty"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()

			storage, err := NewAWSS3Storage(ctx)
			require.NoError(t, err)

			data := io.NopCloser(strings.NewReader("hello test"))
			writeErr := storage.Write(ctx, data, test.input)

			require.Error(t, writeErr)
			assert.Contains(t, writeErr.Error(), test.expectedErr)
		})
	}
}

type MockTransferClient struct {
	mock.Mock
}

func (m *MockTransferClient) UploadObject(ctx context.Context, input *transfermanager.UploadObjectInput, opts ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	args := m.Called(ctx, input, opts)

	if uploadObjectOutput := args.Get(0); uploadObjectOutput != nil {
		return uploadObjectOutput.(*transfermanager.UploadObjectOutput), args.Error(1)
	}

	return nil, args.Error(1)
}

func (m *MockTransferClient) GetObject(ctx context.Context, input *transfermanager.GetObjectInput, opts ...func(*transfermanager.Options)) (*transfermanager.GetObjectOutput, error) {
	args := m.Called(ctx, input, opts)

	if getObjectOutput := args.Get(0); getObjectOutput != nil {
		return getObjectOutput.(*transfermanager.GetObjectOutput), args.Error(1)
	}

	return nil, args.Error(1)
}

func TestAWSS3StorageWriteUploadError(t *testing.T) {
	expectedErr := errors.New("upload error")

	s3ClientMock := new(MockTransferClient)
	s3ClientMock.Test(t)
	s3ClientMock.On("UploadObject", mock.Anything, mock.Anything, mock.Anything).Return(nil, expectedErr)

	storage, _ := NewAWSS3Storage(t.Context())
	storage.s3Client = s3ClientMock

	reader := io.NopCloser(strings.NewReader("hello world"))
	err := storage.Write(t.Context(), reader, "s3://hello/world")

	assert.Equal(t, expectedErr, err)
}

func TestAwsS3StorageWriteSuccess(t *testing.T) {
	inputData := io.NopCloser(strings.NewReader("my data"))

	uploadObjectInput := &transfermanager.UploadObjectInput{
		Bucket: new("bucket"),
		Key:    new("path"),
		Body:   inputData,
	}

	s3ClientMock := new(MockTransferClient)
	s3ClientMock.Test(t)
	s3ClientMock.On("UploadObject", mock.Anything, uploadObjectInput, mock.Anything).Return(&transfermanager.UploadObjectOutput{}, nil)

	storage, err := NewAWSS3Storage(t.Context())
	require.NoError(t, err)

	storage.s3Client = s3ClientMock

	err = storage.Write(t.Context(), inputData, "s3://bucket/path")
	assert.NoError(t, err)
}

func TestAwsS3StorageReadBucketInvalid(t *testing.T) {

	tests := []struct {
		name        string
		input       string
		expectedErr string
	}{
		{name: "not_s3_path", input: "not_s3_path", expectedErr: "should be an s3 address"},
		{name: "empty_bucket", input: "s3:///my_path", expectedErr: "s3 bucket cannot be empty"},
		{name: "empty_path", input: "s3://bucket", expectedErr: "s3 key cannot be empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()

			storage, err := NewAWSS3Storage(ctx)
			require.NoError(t, err)
			storage.s3Client = &MockTransferClient{}

			_, err = storage.Read(ctx, tt.input)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestAwsS3StorageReadBucketGetObjectError(t *testing.T) {
	ctx := t.Context()

	expectedErr := errors.New("get object failed")

	mockTransfer := &MockTransferClient{}
	mockTransfer.On("GetObject", mock.Anything, mock.Anything, mock.Anything).Return(nil, expectedErr)

	storage, err := NewAWSS3Storage(ctx)
	require.NoError(t, err)
	storage.s3Client = mockTransfer

	result, err := storage.Read(ctx, "s3://my_bucket/path")
	assert.Equal(t, expectedErr, err)
	assert.Nil(t, result)
}

func TestAwsS3StorageReadBucketGetObjectSuccess(t *testing.T) {
	ctx := t.Context()

	dummyData := "dummy data"
	reader := strings.NewReader(dummyData)

	mockTransfer := &MockTransferClient{}
	mockTransfer.On("GetObject", mock.Anything, mock.Anything, mock.Anything).Return(&transfermanager.GetObjectOutput{Body: reader}, nil)

	storage, err := NewAWSS3Storage(ctx)
	require.NoError(t, err)
	storage.s3Client = mockTransfer

	returnedReader, err := storage.Read(ctx, "s3://bucket/input")
	require.NoError(t, err)

	b, err := io.ReadAll(returnedReader)
	require.NoError(t, err)

	assert.Equal(t, dummyData, string(b))
}
