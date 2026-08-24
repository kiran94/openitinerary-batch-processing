package queue

import (
	"fmt"

	"github.com/kiran94/openitinerary-batch-processing/internal/job"
)

type BatchQueue interface {
	Enqueue(ticket *job.Ticket) error
}

type InMemoryQueue struct {
}

func NewInMemoryQueue() BatchQueue {
	return &InMemoryQueue{}
}

func (q *InMemoryQueue) Enqueue(ticket *job.Ticket) error {

	// TODO: This is just a placeholder right now
	fmt.Printf("Enqueued job: %v \n", ticket.ID)
	return nil
}

func (q *InMemoryQueue) Close() error {
	return nil
}
