package job

import (
	"gorm.io/gorm"
)

type JobCreator interface {
	Create(j *Ticket) error
}

type Database interface {
	Create(value interface{}) (tx *gorm.DB)
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
