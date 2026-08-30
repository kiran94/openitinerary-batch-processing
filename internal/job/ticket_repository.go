package job

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type TicketRepository interface {
	Create(ticket *Ticket) error
	Get(id uuid.UUID) (*Ticket, error)
	Update(ticket *Ticket) error
	MarkFailed(id uuid.UUID, reason TicketStatusReason, detail string) (bool, error)
}

type Database interface {
	Create(value interface{}) (tx *gorm.DB)
	Find(dest interface{}, conditions ...interface{}) (tx *gorm.DB)
	Model(value interface{}) (tx *gorm.DB)
}

func NewTicketRepository(db Database) TicketRepository {
	return &ticketRepository{db}
}

type ticketRepository struct {
	db Database
}

func (r *ticketRepository) Create(ticket *Ticket) error {
	if result := r.db.Create(ticket); result.Error != nil {
		return result.Error
	}

	return nil
}

var ErrUnsupportedTicketStatusReason = errors.New("unsupported ticket status reason")

func (r *ticketRepository) MarkFailed(id uuid.UUID, reason TicketStatusReason, detail string) (bool, error) {
	switch reason {
	case EnqueueFailed:
		return r.markEnqueueFailed(id, detail)
	default:
		return false, fmt.Errorf("%w: %s", ErrUnsupportedTicketStatusReason, reason)
	}
}

// markEnqueueFailed atomically transitions a queued ticket to failed. The
// conditional partial update avoids Save overwriting a newer status set by a
// worker that may have received the message before the enqueue error returned.
// On the queue side, it's possible for an acknowledgement to be lost even
// if the message was actually submitted to the queue.
func (r *ticketRepository) markEnqueueFailed(id uuid.UUID, detail string) (bool, error) {

	result := r.db.
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

func (r *ticketRepository) Get(id uuid.UUID) (*Ticket, error) {
	ticket := &Ticket{}

	result := r.db.Find(ticket, []string{id.String()})
	if result.Error != nil {
		return nil, result.Error
	}

	if result.RowsAffected == 0 {
		return nil, ErrRecordNotFound
	}

	return ticket, nil
}

func (r *ticketRepository) Update(ticket *Ticket) error {
	result := r.db.Model(ticket).Select("*").Updates(ticket)
	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected == 0 {
		return ErrRecordNotFound
	}

	return nil
}
