package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"

	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type Ready interface {
	Ready(ctx context.Context) error
}

type BatchQueue interface {
	Ready
	Enqueue(ctx context.Context, ticket *job.Ticket) error
	Dequeue(ctx context.Context) (*DequeueResult, error)
	DeleteMessage(ctx context.Context, queueReference string) error
}

type Queue struct {
	Queue  Queuer
	config config.Config
	logger logging.Logger
}

type Queuer interface {
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	GetQueueAttributes(ctx context.Context, params *sqs.GetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
}

func NewQueue(ctx context.Context, c config.Config, logger logging.Logger) (BatchQueue, error) {
	con, err := awsConfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	// ensure that the queue URL is present
	_ = c.GetQueueURL()

	client := sqs.NewFromConfig(con)
	return &Queue{Queue: client, config: c, logger: logger}, nil
}

type EnqueuePayload struct {
	TicketID  string `json:"ticket_id"`
	RequestID string `json:"request_id"`
}

func (q *Queue) Ready(ctx context.Context) error {
	return checkVisibilityTimeout(ctx, q.config, q.Queue)
}

func (q *Queue) Enqueue(ctx context.Context, ticket *job.Ticket) error {
	logger := q.logger.WithContext(ctx)
	ep := EnqueuePayload{TicketID: ticket.ID.String()}

	if requestID, ok := logging.GetRequestID(ctx); ok {
		ep.RequestID = requestID
	}

	buffer, err := json.Marshal(ep)
	if err != nil {
		return err
	}
	bufferString := string(buffer)

	result, err := q.Queue.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               new(q.config.GetQueueURL()),
		MessageGroupId:         new(ticket.ID.String()),
		MessageDeduplicationId: new(ticket.ID.String()),
		MessageBody:            &bufferString,
	})

	if err != nil {
		return err
	}

	logger.Debug(
		"queue enqueue succeeded",
		"component", logging.ComponentQueue,
		"operation", logging.OperationEnqueue,
		"queue_message_id", aws.ToString(result.MessageId),
	)
	return nil
}

type DequeueResult struct {
	Result EnqueuePayload

	// Some reference from the queue so that the message can be
	// removed from the queue upon processing
	// in AWS SQS, this is the receipt handle.
	QueueReference *string
}

func (q *Queue) Dequeue(ctx context.Context) (*DequeueResult, error) {
	logger := q.logger.WithContext(ctx)
	result, err := q.Queue.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:                new(q.config.GetQueueURL()),
		MaxNumberOfMessages:     1,
		WaitTimeSeconds:         q.config.GetQueuePollingTime(),
		ReceiveRequestAttemptId: new(uuid.NewString()),
	})

	if err != nil {
		return nil, err
	}

	if len(result.Messages) == 0 {
		return nil, nil
	}

	message := result.Messages[0]
	if message.ReceiptHandle == nil {
		err := errors.New("receipt handle was nil")
		logger.Error(
			"queue message invalid",
			"component", logging.ComponentQueue,
			"operation", logging.OperationDequeue,
			"message_id", aws.ToString(message.MessageId),
			"reason", "nil_receipt_handle",
			"error", err,
		)
		return nil, err
	}

	// Preserve the receipt handle when the body is malformed so the worker can
	// delete the message instead of allowing it to retry until retention.
	dequeueResult := &DequeueResult{
		QueueReference: message.ReceiptHandle,
	}

	if message.Body == nil {
		err := errors.New("message body was nil")
		logger.Error(
			"queue message invalid",
			"component", logging.ComponentQueue,
			"operation", logging.OperationDequeue,
			"message_id", aws.ToString(message.MessageId),
			"reason", "nil_body",
			"error", err,
		)
		return dequeueResult, err
	}

	if err := json.Unmarshal([]byte(*message.Body), &dequeueResult.Result); err != nil {
		logger.Error(
			"queue message invalid",
			"component", logging.ComponentQueue,
			"operation", logging.OperationDequeue,
			"message_id", aws.ToString(message.MessageId),
			"reason", "invalid_json",
			"error", err,
		)
		return dequeueResult, err
	}

	return dequeueResult, nil
}

func (q *Queue) DeleteMessage(ctx context.Context, queueReference string) error {
	if queueReference == "" {
		return errors.New("queueReference was empty")
	}

	_, err := q.Queue.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      new(q.config.GetQueueURL()),
		ReceiptHandle: &queueReference,
	})

	if err != nil {
		return err
	}

	return nil
}

func (q *Queue) Close() error {
	return nil
}

func checkVisibilityTimeout(ctx context.Context, c config.Config, client Queuer) error {
	result, err := client.GetQueueAttributes(
		ctx,
		&sqs.GetQueueAttributesInput{
			AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameVisibilityTimeout},
			QueueUrl:       new(c.GetQueueURL()),
		},
	)
	if err != nil {
		return err
	}

	v, ok := result.Attributes[string(types.QueueAttributeNameVisibilityTimeout)]
	if !ok {
		return errors.New("VisibilityTimeout was not found in the SQS attribute response")
	}

	visibilityTimeoutSeconds, err := strconv.Atoi(v)
	if err != nil {
		return err
	}

	const buffer = 60 // visibility timeout safety margin
	workerTimeoutSeconds := c.GetWorkerTimeoutSeconds()

	workerTimeoutWithBuffer := workerTimeoutSeconds + buffer

	if workerTimeoutWithBuffer > visibilityTimeoutSeconds {
		return fmt.Errorf(
			"worker timeout must be at most %d seconds: worker_timeout=%d, safety_margin=%d, visibility_timeout=%d",
			visibilityTimeoutSeconds-buffer,
			workerTimeoutSeconds,
			buffer,
			visibilityTimeoutSeconds,
		)
	}

	return nil
}
