package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"

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
}

type Queuer interface {
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

func NewQueue(ctx context.Context, c config.Config) (BatchQueue, error) {
	con, err := awsConfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	// ensure that the queue URL is present
	_ = c.GetQueueURL()

	client := sqs.NewFromConfig(con)
	return &Queue{Queue: client, config: c}, nil
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

	fmt.Printf("Submitted Job %v with Message Id: %v", ticket.ID, *result.MessageId)
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
		QueueUrl:            new(q.config.GetQueueURL()),
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     q.config.GetQueuePollingTime(),
	})

	if err != nil {
		return nil, err
	}

	if len(result.Messages) == 0 {
		return nil, nil
	}

	message := result.Messages[0]
	if message.Body == nil {
		return nil, errors.New("message body was nil")
	}

	if message.ReceiptHandle == nil {
		return nil, errors.New("receipt handle was nil")
	}

	ticket := new(job.Ticket)
	if err := json.Unmarshal([]byte(*message.Body), ticket); err != nil {
		return nil, err
	}

	return &DequeueResult{*ticket, message.ReceiptHandle}, nil
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
