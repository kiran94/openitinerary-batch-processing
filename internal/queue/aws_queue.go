package queue

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"

	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type BatchQueue interface {
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

func (q *Queue) Enqueue(ctx context.Context, ticket *job.Ticket) error {

	buffer, err := json.Marshal(ticket)
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

	q.logger.Debug(
		"queue enqueue succeeded",
		"component", logging.ComponentQueue,
		"ticket_id", ticket.ID.String(),
		"queue_message_id", aws.ToString(result.MessageId),
	)
	return nil
}

type DequeueResult struct {
	Result job.Ticket

	// Some reference from the queue so that the message can be
	// removed from the queue upon processing
	// in AWS SQS, this is the receipt handle.
	QueueReference *string
}

func (q *Queue) Dequeue(ctx context.Context) (*DequeueResult, error) {
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
		q.logger.Error(
			"queue message invalid",
			"component", logging.ComponentQueue,
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
		q.logger.Error(
			"queue message invalid",
			"component", logging.ComponentQueue,
			"message_id", aws.ToString(message.MessageId),
			"reason", "nil_body",
			"error", err,
		)
		return dequeueResult, err
	}

	if err := json.Unmarshal([]byte(*message.Body), &dequeueResult.Result); err != nil {
		q.logger.Error(
			"queue message invalid",
			"component", logging.ComponentQueue,
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
