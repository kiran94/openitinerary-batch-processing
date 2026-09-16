package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"go.opentelemetry.io/otel/trace"
)

type StatusService interface {
	GetStatus(ctx context.Context, ticketID uuid.UUID) (*job.Ticket, error)
}

type statusService struct {
	job    job.TicketRepository
	tracer trace.Tracer
}

func NewStatusService(job job.TicketRepository, tracer trace.Tracer) StatusService {
	return &statusService{job: job, tracer: tracer}
}

func (ss *statusService) GetStatus(ctx context.Context, ticketID uuid.UUID) (*job.Ticket, error) {
	ctx, span := ss.tracer.Start(ctx, "GetStatus")
	defer span.End()

	if ticket, err := ss.job.Get(ctx, ticketID); err != nil {
		return nil, err
	} else {
		return ticket, nil
	}
}
