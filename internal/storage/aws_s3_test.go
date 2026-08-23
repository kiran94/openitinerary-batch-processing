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

type MockUploader struct {
	mock.Mock
}

func (m *MockUploader) UploadObject(ctx context.Context, input *transfermanager.UploadObjectInput, opts ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	args := m.Called(ctx, input, opts)

	if uploadObjectOutput := args.Get(0); uploadObjectOutput != nil {
		return uploadObjectOutput.(*transfermanager.UploadObjectOutput), args.Error(1)
	}

	return nil, args.Error(1)
}

func TestAWSS3StorageWriteUploadError(t *testing.T) {
	expectedErr := errors.New("upload error")

	uploaderMock := new(MockUploader)
	uploaderMock.Test(t)
	uploaderMock.On("UploadObject", mock.Anything, mock.Anything, mock.Anything).Return(nil, expectedErr)

	storage, _ := NewAWSS3Storage(t.Context())
	storage.s3Client = uploaderMock

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

	uploaderMock := new(MockUploader)
	uploaderMock.Test(t)
	uploaderMock.On("UploadObject", mock.Anything, uploadObjectInput, mock.Anything).Return(&transfermanager.UploadObjectOutput{}, nil)

	storage, err := NewAWSS3Storage(t.Context())
	require.NoError(t, err)

	storage.s3Client = uploaderMock

	err = storage.Write(t.Context(), inputData, "s3://bucket/path")
	assert.NoError(t, err)
}
