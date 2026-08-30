package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewQueue(t *testing.T) {
	mockConfig := MockConfig{}
	mockConfig.On("GetQueueURL").Return("my_queue")

	queue, err := NewQueue(t.Context(), &mockConfig, &MockLogger{})

	require.NoError(t, err)
	assert.NotNil(t, queue)
}

func TestEnqueue(t *testing.T) {
	ctx := t.Context()
	queueURL := "https://sqs.ap-southeast-1.amazonaws.com/123456789012/jobs.fifo"
	ticket := job.NewTicket()

	expectedMessageBody, err := json.Marshal(EnqueuePayload{TicketID: ticket.ID.String()})
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

	batchQueue := &Queue{Queue: mockQueue, config: mockConfig, logger: &MockLogger{}}
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

	batchQueue := &Queue{Queue: mockQueue, config: mockConfig, logger: &MockLogger{}}
	err := batchQueue.Enqueue(ctx, ticket)

	assert.Equal(t, expectedErr, err)
}

func TestDequeue(t *testing.T) {
	ctx := t.Context()
	queuePollingTime := int32(20)
	expectedPayload := EnqueuePayload{TicketID: uuid.NewString()}

	messageBody, err := json.Marshal(expectedPayload)
	require.NoError(t, err)
	messageBodyString := string(messageBody)
	receiptHandle := "handle1"

	queueURL := "myQueue"
	mockConfig := new(MockConfig)
	mockConfig.On("GetQueueURL").Return(queueURL).Once()
	mockConfig.On("GetQueuePollingTime").Return(queuePollingTime).Once()

	mockQueue := new(MockBatchQueue)
	mockQueue.
		On("ReceiveMessage", ctx, mock.AnythingOfType("*sqs.ReceiveMessageInput"), mock.Anything).
		Run(func(args mock.Arguments) {
			input, ok := args.Get(1).(*sqs.ReceiveMessageInput)

			require.True(t, ok)
			require.NotNil(t, input.QueueUrl)

			assert.Equal(t, queueURL, *input.QueueUrl)
			assert.Equal(t, int32(1), input.MaxNumberOfMessages)
			assert.Equal(t, queuePollingTime, input.WaitTimeSeconds)
		}).
		Return(&sqs.ReceiveMessageOutput{
			Messages: []types.Message{
				{
					Body:          &messageBodyString,
					ReceiptHandle: &receiptHandle,
				},
			},
		}, nil).
		Once()

	queue := &Queue{Queue: mockQueue, config: mockConfig, logger: &MockLogger{}}
	result, err := queue.Dequeue(ctx)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.QueueReference)

	assert.Equal(t, expectedPayload, result.Result)
	assert.Equal(t, receiptHandle, *result.QueueReference)

	mockConfig.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestDequeueNilBody(t *testing.T) {
	ctx := t.Context()
	receiptHandle := "handle1"

	queueURL := "myQueue"
	mockConfig := new(MockConfig)
	mockConfig.On("GetQueueURL").Return(queueURL).Once()
	mockConfig.On("GetQueuePollingTime").Return(int32(20)).Once()

	mockQueue := new(MockBatchQueue)
	mockQueue.
		On("ReceiveMessage", ctx, mock.AnythingOfType("*sqs.ReceiveMessageInput"), mock.Anything).
		Return(&sqs.ReceiveMessageOutput{
			Messages: []types.Message{
				{
					Body:          nil,
					ReceiptHandle: &receiptHandle,
				},
			},
		}, nil).
		Once()

	queue := &Queue{Queue: mockQueue, config: mockConfig, logger: &MockLogger{}}
	result, err := queue.Dequeue(ctx)

	require.Error(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.QueueReference)

	assert.Contains(t, err.Error(), "message body was nil")
	assert.Equal(t, receiptHandle, *result.QueueReference)

	mockConfig.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestDequeueInvalidJSON(t *testing.T) {
	ctx := t.Context()
	messageBody := "invalid JSON"
	receiptHandle := "handle1"

	queueURL := "myQueue"
	mockConfig := new(MockConfig)
	mockConfig.On("GetQueueURL").Return(queueURL).Once()
	mockConfig.On("GetQueuePollingTime").Return(int32(20)).Once()

	mockQueue := new(MockBatchQueue)
	mockQueue.
		On("ReceiveMessage", ctx, mock.AnythingOfType("*sqs.ReceiveMessageInput"), mock.Anything).
		Return(&sqs.ReceiveMessageOutput{
			Messages: []types.Message{
				{
					Body:          &messageBody,
					ReceiptHandle: &receiptHandle,
				},
			},
		}, nil).
		Once()

	queue := &Queue{Queue: mockQueue, config: mockConfig, logger: &MockLogger{}}
	result, err := queue.Dequeue(ctx)

	require.Error(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.QueueReference)

	assert.Equal(t, receiptHandle, *result.QueueReference)

	mockConfig.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestDequeueNilReceiptHandle(t *testing.T) {
	ctx := t.Context()
	payload := EnqueuePayload{TicketID: uuid.NewString()}

	messageBody, err := json.Marshal(payload)
	require.NoError(t, err)
	messageBodyString := string(messageBody)

	queueURL := "myQueue"
	mockConfig := new(MockConfig)
	mockConfig.On("GetQueueURL").Return(queueURL).Once()
	mockConfig.On("GetQueuePollingTime").Return(int32(20)).Once()

	mockQueue := new(MockBatchQueue)
	mockQueue.
		On("ReceiveMessage", ctx, mock.AnythingOfType("*sqs.ReceiveMessageInput"), mock.Anything).
		Return(&sqs.ReceiveMessageOutput{
			Messages: []types.Message{
				{
					Body:          &messageBodyString,
					ReceiptHandle: nil,
				},
			},
		}, nil).
		Once()

	queue := &Queue{Queue: mockQueue, config: mockConfig, logger: &MockLogger{}}
	result, err := queue.Dequeue(ctx)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "receipt handle was nil")

	mockConfig.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestDequeueReceiveMessageError(t *testing.T) {
	ctx := t.Context()

	queueURL := "myQueue"
	mockConfig := new(MockConfig)
	mockConfig.On("GetQueueURL").Return(queueURL).Once()
	mockConfig.On("GetQueuePollingTime").Return(int32(20)).Once()

	expectedErr := errors.New("failed to receive message")
	mockQueue := new(MockBatchQueue)
	mockQueue.
		On("ReceiveMessage", ctx, mock.AnythingOfType("*sqs.ReceiveMessageInput"), mock.Anything).
		Return(nil, expectedErr).
		Once()

	queue := &Queue{Queue: mockQueue, config: mockConfig, logger: &MockLogger{}}
	result, err := queue.Dequeue(ctx)

	assert.Nil(t, result)
	assert.Equal(t, expectedErr, err)

	mockConfig.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestDequeueEmptyQueue(t *testing.T) {
	ctx := t.Context()

	queueURL := "myQueue"
	mockConfig := new(MockConfig)
	mockConfig.On("GetQueueURL").Return(queueURL).Once()
	mockConfig.On("GetQueuePollingTime").Return(int32(20)).Once()

	mockQueue := new(MockBatchQueue)
	mockQueue.
		On("ReceiveMessage", ctx, mock.AnythingOfType("*sqs.ReceiveMessageInput"), mock.Anything).
		Return(&sqs.ReceiveMessageOutput{}, nil).
		Once()

	queue := &Queue{Queue: mockQueue, config: mockConfig, logger: &MockLogger{}}
	result, err := queue.Dequeue(ctx)

	require.NoError(t, err)
	assert.Nil(t, result)

	mockConfig.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestDeleteMessage(t *testing.T) {
	ctx := t.Context()
	queueURL := "myQueue"
	queueReference := "receipt-handle"

	mockConfig := new(MockConfig)
	mockConfig.On("GetQueueURL").Return(queueURL).Once()

	mockQueue := new(MockBatchQueue)
	mockQueue.
		On("DeleteMessage", ctx, mock.AnythingOfType("*sqs.DeleteMessageInput"), mock.Anything).
		Run(func(args mock.Arguments) {
			input, ok := args.Get(1).(*sqs.DeleteMessageInput)

			require.True(t, ok)
			assert.Equal(t, queueURL, *input.QueueUrl)
			assert.Equal(t, queueReference, *input.ReceiptHandle)
		}).
		Return(&sqs.DeleteMessageOutput{}, nil).
		Once()

	queue := &Queue{Queue: mockQueue, config: mockConfig, logger: &MockLogger{}}
	err := queue.DeleteMessage(ctx, queueReference)

	require.NoError(t, err)
	mockConfig.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

func TestDeleteMessageEmptyQueueReference(t *testing.T) {
	mockConfig := new(MockConfig)
	mockQueue := new(MockBatchQueue)
	queue := &Queue{Queue: mockQueue, config: mockConfig, logger: &MockLogger{}}

	err := queue.DeleteMessage(t.Context(), "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "queueReference was empty")
	mockConfig.AssertNotCalled(t, "GetQueueURL")
	mockQueue.AssertNotCalled(t, "DeleteMessage", mock.Anything, mock.Anything, mock.Anything)
}

func TestDeleteMessageError(t *testing.T) {
	ctx := t.Context()
	queueURL := "myQueue"
	queueReference := "receipt-handle"

	mockConfig := new(MockConfig)
	mockConfig.On("GetQueueURL").Return(queueURL).Once()

	expectedErr := errors.New("failed to delete message")
	mockQueue := new(MockBatchQueue)
	mockQueue.
		On("DeleteMessage", ctx, mock.AnythingOfType("*sqs.DeleteMessageInput"), mock.Anything).
		Return(nil, expectedErr).
		Once()

	queue := &Queue{Queue: mockQueue, config: mockConfig, logger: &MockLogger{}}
	err := queue.DeleteMessage(ctx, queueReference)

	assert.Equal(t, expectedErr, err)
	mockConfig.AssertExpectations(t)
	mockQueue.AssertExpectations(t)
}

// MOCKS

// Compile Time Interface Assertions
var (
	_ config.Config = (*MockConfig)(nil)
)

type MockConfig struct {
	mock.Mock
}

func (m *MockConfig) GetEnvironment() string {
	return m.Called().String(0)
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

func (m *MockConfig) GetMaximumAdapterResponseSizeBytes() int64 {
	return m.Called().Get(0).(int64)
}

func (m *MockConfig) GetBucket() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetBucketPath() string {
	return m.Called().String(0)
}

func (m *MockConfig) GetS3UploadConcurrency() int {
	return m.Called().Int(0)
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

func (m *MockConfig) GetQueuePollingTime() int32 {
	return m.Called().Get(0).(int32)
}

func (m *MockConfig) GetWorkerTimeoutSeconds() int {
	return m.Called().Int(0)
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

func (m *MockBatchQueue) ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	args := m.Called(ctx, params, optFns)

	if args.Get(0) != nil {
		return args.Get(0).(*sqs.ReceiveMessageOutput), args.Error(1)
	}

	return nil, args.Error(1)
}

func (m *MockBatchQueue) DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	args := m.Called(ctx, params, optFns)

	if args.Get(0) != nil {
		return args.Get(0).(*sqs.DeleteMessageOutput), args.Error(1)
	}

	return nil, args.Error(1)
}

type MockLogger struct{}

func (m *MockLogger) Debug(message string, args ...any) {}

func (m *MockLogger) Info(message string, args ...any) {}

func (m *MockLogger) Warn(message string, args ...any) {}

func (m *MockLogger) Error(message string, args ...any) {}
