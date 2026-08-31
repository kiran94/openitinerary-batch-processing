package job

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Ticket struct {
	ID           uuid.UUID `json:"id"`
	DocumentPath string    `json:"document_path"`
	ContentType  string    `json:"content_type"`

	Status       TicketStatus        `json:"status" gorm:"type:varchar(16);default:'queued';not null"`
	StatusReason *TicketStatusReason `json:"status_reason,omitempty" gorm:"type:varchar(32)"`
	Error        *string             `json:"-" gorm:"type:text"`
	AttemptCount uint16              `json:"-"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type TicketStatus string

const (
	Queued     TicketStatus = "queued"
	Processing TicketStatus = "processing"
	Complete   TicketStatus = "complete"
	Failed     TicketStatus = "failed"
)

type TicketStatusReason string

const (
	// FAILED REASONS
	EnqueueFailed      TicketStatusReason = "enqueue_failed"
	StorageReadFailed  TicketStatusReason = "storage_read_failed"
	StorageReadNil     TicketStatusReason = "storage_read_nil"
	ConvertFailed      TicketStatusReason = "convert_failed"
	ResponseReaderNil  TicketStatusReason = "response_reader_nil"
	StorageWriteFailed TicketStatusReason = "storage_write_failed"
)

func NewTicket() *Ticket {
	return &Ticket{
		ID:           uuid.New(),
		DocumentPath: "",
		ContentType:  "",
		Status:       Queued,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
}

func (t Ticket) String() string {
	return fmt.Sprintf("ticket: %v", t.ID.String())
}
