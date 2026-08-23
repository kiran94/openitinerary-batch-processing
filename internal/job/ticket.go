package job

import (
	"time"

	"github.com/google/uuid"
)

type Ticket struct {
	ID           uuid.UUID
	DocumentPath string
	Status       TicketStatus `gorm:"type:varchar(16);default:'queued';not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

type TicketStatus string

const (
	Queued     TicketStatus = "queued"
	Processing TicketStatus = "processing"
	Complete   TicketStatus = "complete"
	Failed     TicketStatus = "failed"
)

func NewTicket(documentPath string) *Ticket {
	return &Ticket{
		ID:           uuid.New(),
		DocumentPath: documentPath,
		Status:       Queued,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

}
