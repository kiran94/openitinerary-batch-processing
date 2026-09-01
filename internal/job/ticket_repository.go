package job

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
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

func NewTicketRepository(db Database) TicketRepository {
	return &ticketRepository{db}
}

type ticketRepository struct {
	db Database
}

func (r *ticketRepository) Create(ctx context.Context, ticket *Ticket) error {
	if result := r.db.WithContext(ctx).Create(ticket); result.Error != nil {
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
	ticket := &Ticket{}

	result := r.db.WithContext(ctx).Find(ticket, []string{id.String()})
	if result.Error != nil {
		return nil, result.Error
	}

	if result.RowsAffected == 0 {
		return nil, ErrRecordNotFound
	}

	return ticket, nil
}

func (r *ticketRepository) Update(ctx context.Context, ticket *Ticket) error {
	result := r.db.WithContext(ctx).Model(ticket).Select("*").Updates(ticket)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return ErrRecordNotFound
	}

	return nil
}
