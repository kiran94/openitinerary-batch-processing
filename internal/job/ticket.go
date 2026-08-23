package job

import (
	"time"

	"github.com/google/uuid"
)

type Ticket struct {
	ID           uuid.UUID    `json:"id"`
	DocumentPath string       `json:"document_path"`
	Status       TicketStatus `json:"status" gorm:"type:varchar(16);default:'queued';not null"`

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

func NewTicket() *Ticket {
	return &Ticket{
		ID:           uuid.New(),
		DocumentPath: "",
		Status:       Queued,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

}
