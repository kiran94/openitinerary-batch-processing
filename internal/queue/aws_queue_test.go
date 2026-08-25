package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewQueue(t *testing.T) {
	mockConfig := MockConfig{}
	queue, err := NewQueue(t.Context(), &mockConfig)

	require.NoError(t, err)
	assert.NotNil(t, queue)
}

func TestEnqueue(t *testing.T) {
	ctx := t.Context()
	queueURL := "https://sqs.ap-southeast-1.amazonaws.com/123456789012/jobs.fifo"
	ticket := job.NewTicket()

	expectedMessageBody, err := json.Marshal(ticket)
	require.NoError(t, err)

	mockConfig := new(MockConfig)
	mockConfig.On("GetQueueURL").Return(queueURL).Once()

	mockQueue := new(MockBatchQueue)
	var sentMessage *sqs.SendMessageInput
	mockQueue.
		On("SendMessage", ctx, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			sentMessage = args.Get(1).(*sqs.SendMessageInput)
		}).
		Return(&sqs.SendMessageOutput{MessageId: new("message-id")}, nil).
		Once()

	batchQueue := &Queue{Queue: mockQueue, config: mockConfig}
	err = batchQueue.Enqueue(ctx, ticket)

	require.NoError(t, err)
	require.NotNil(t, sentMessage)

	assert.Equal(t, queueURL, *sentMessage.QueueUrl)
	assert.Equal(t, string(expectedMessageBody), *sentMessage.MessageBody)
	assert.Equal(t, ticket.ID.String(), *sentMessage.MessageGroupId)
	assert.Equal(t, ticket.ID.String(), *sentMessage.MessageDeduplicationId)

	mockConfig.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestEnqueueSendMessageError(t *testing.T) {
	ctx := t.Context()
	queueURL := "https://sqs.ap-southeast-1.amazonaws.com/123456789012/jobs.fifo"
	ticket := job.NewTicket()

	mockConfig := new(MockConfig)
	mockConfig.On("GetQueueURL").Return(queueURL).Once()

	expectedErr := errors.New("failed to send message")
	mockQueue := new(MockBatchQueue)
	mockQueue.
		On("SendMessage", ctx, mock.Anything, mock.Anything).
		Return(nil, expectedErr).
		Once()

	batchQueue := &Queue{Queue: mockQueue, config: mockConfig}
	err := batchQueue.Enqueue(ctx, ticket)

	assert.Equal(t, expectedErr, err)
}

// MOCKS

// Compile Time Interface Assertions
var (
	_ config.Config = (*MockConfig)(nil)
)

type MockConfig struct {
	mock.Mock
}

func (m *MockConfig) GetPort() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetServerReadTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *MockConfig) GetServerWriteTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *MockConfig) GetServerIdleTimeout() time.Duration {
	return m.Called().Get(0).(time.Duration)
}

func (m *MockConfig) GetMaximumAllowedRequestSizeMb() int64 {
	return m.Called().Get(0).(int64)
}

func (m *MockConfig) GetMaximumBufferMultipartForm() int64 {
	return m.Called().Get(0).(int64)
}

func (m *MockConfig) GetBucket() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetBucketPath() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetDocumentPath(bucket, bucketPath, jobID, fileType string) string {
	return m.Called(bucket, bucketPath, jobID, fileType).String(0)
}

func (m *MockConfig) GetOutputPath(bucket, bucketPath, jobID string) string {
	return m.Called(bucket, bucketPath, jobID).String(0)
}

func (m *MockConfig) GetQueueURL() string {
	return m.Called().String(0)
}

type MockBatchQueue struct {
	mock.Mock
}

func (m *MockBatchQueue) Enqueue(ctx context.Context, ticket *job.Ticket) error {
	return m.Called(ticket).Error(0)
}

func (m *MockBatchQueue) SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	args := m.Called(ctx, params, optFns)

	sendMessageOutput := args.Get(0)
	if sendMessageOutput != nil {
		return args.Get(0).(*sqs.SendMessageOutput), args.Error(1)
	}

	return nil, args.Error(1)
}
