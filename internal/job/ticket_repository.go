package job

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

type TicketRepository interface {
	Create(ctx context.Context, ticket *Ticket) error
	Get(ctx context.Context, id uuid.UUID) (*Ticket, error)
	Update(ctx context.Context, ticket *Ticket) error
	MarkFailed(ctx context.Context, id uuid.UUID, reason TicketStatusReason, detail string) (bool, error)
}

type Database interface {
	Create(value interface{}) (tx *gorm.DB)
	Find(dest interface{}, conditions ...interface{}) (tx *gorm.DB)
	Model(value interface{}) (tx *gorm.DB)
	WithContext(ctx context.Context) *gorm.DB
}

func NewTicketRepository(db Database, tracer trace.Tracer) TicketRepository {
	return &ticketRepository{db, tracer}
}

type ticketRepository struct {
	db     Database
	tracer trace.Tracer
}

func (r *ticketRepository) Create(ctx context.Context, ticket *Ticket) error {
	ctx, span := r.tracer.Start(ctx, string(telemetry.TracingSpanCreateTicket))
	defer span.End()

	if result := r.db.WithContext(ctx).Create(ticket); result.Error != nil {
		span.RecordError(result.Error)
		return result.Error
	}

	return nil
}

var ErrUnsupportedTicketStatusReason = errors.New("unsupported ticket status reason")

func (r *ticketRepository) MarkFailed(
	ctx context.Context,
	id uuid.UUID,
	reason TicketStatusReason,
	detail string,
) (bool, error) {
	switch reason {
	case EnqueueFailed:
		return r.markEnqueueFailed(ctx, id, detail)
	default:
		return false, fmt.Errorf("%w: %s", ErrUnsupportedTicketStatusReason, reason)
	}
}

// markEnqueueFailed atomically transitions a queued ticket to failed. The
// conditional partial update avoids Save overwriting a newer status set by a
// worker that may have received the message before the enqueue error returned.
// On the queue side, it's possible for an acknowledgement to be lost even
// if the message was actually submitted to the queue.
func (r *ticketRepository) markEnqueueFailed(ctx context.Context, id uuid.UUID, detail string) (bool, error) {

	result := r.db.
		WithContext(ctx).
		Model(&Ticket{}).
		Where("id = ? AND status = ?", id, Queued).
		Updates(map[string]interface{}{
			"status":        Failed,
			"status_reason": EnqueueFailed,
			"error":         detail,
		})
	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected == 1, nil
}

var ErrRecordNotFound = errors.New("record not found")

func (r *ticketRepository) Get(ctx context.Context, id uuid.UUID) (*Ticket, error) {
	ctx, span := r.tracer.Start(ctx, string(telemetry.TracingSpanGetTicket), trace.WithAttributes(attribute.String("ticket.id", id.String())))
	defer span.End()

	ticket := &Ticket{}

	result := r.db.WithContext(ctx).Find(ticket, []string{id.String()})
	if result.Error != nil {
		span.RecordError(result.Error)
		return nil, result.Error
	}

	if result.RowsAffected == 0 {
		span.AddEvent("ticket not found")
		return nil, ErrRecordNotFound
	}

	return ticket, nil
}

func (r *ticketRepository) Update(ctx context.Context, ticket *Ticket) error {
	ctx, span := r.tracer.Start(ctx, string(telemetry.TracingSpanUpdateTicket), trace.WithAttributes(attribute.String("ticket.id", ticket.ID.String())))
	defer span.End()

	result := r.db.WithContext(ctx).Model(ticket).Select("*").Updates(ticket)
	if result.Error != nil {
		span.RecordError(result.Error)
		return result.Error
	}

	if result.RowsAffected == 0 {
		span.AddEvent("ticket not found")
		return ErrRecordNotFound
	}

	return nil
}
