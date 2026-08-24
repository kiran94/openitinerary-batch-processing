package job

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Ticket struct {
	ID           uuid.UUID    `json:"id"`
	DocumentPath string       `json:"document_path"`
	ContentType  string       `json:"content_type"`
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
		ContentType:  "",
		Status:       Queued,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
}

func (t Ticket) String() string {
	return fmt.Sprintf("ticket: %v", t.ID.String())
}
