package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"

	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type BatchQueue interface {
	Enqueue(ctx context.Context, ticket *job.Ticket) error
}

type Queue struct {
	Queue  Queuer
	config config.Config
}

type Queuer interface {
	SendMessage(ctx context.Context, params *sqs.SendMessageInput, optFns ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

func NewQueue(ctx context.Context, c config.Config) (BatchQueue, error) {
	con, err := awsConfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

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

func (q *Queue) Close() error {
	return nil
}
