package job

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type JobCreator interface {
	Create(j *Ticket) error
	Get(id uuid.UUID) (*Ticket, error)
}

type Database interface {
	Create(value interface{}) (tx *gorm.DB)
	Find(dest interface{}, conditions ...interface{}) (tx *gorm.DB)
}

func NewJobService(db Database) JobCreator {
	return &JobService{db}
}

type JobService struct {
	db Database
}

func (r *JobService) Create(ticket *Ticket) error {
	if result := r.db.Create(ticket); result.Error != nil {
		return result.Error
	}

	return nil
}

func (r *JobService) Get(id uuid.UUID) (*Ticket, error) {
	ticket := NewTicket()
	if result := r.db.Find(ticket, []string{id.String()}); result.Error != nil {
		return nil, result.Error
	}

	return ticket, nil
}
