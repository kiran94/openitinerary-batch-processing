package job

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type MockDatabase struct {
	mock.Mock
}

func (m *MockDatabase) Create(value interface{}) (tx *gorm.DB) {
	args := m.Called(value)
	return args.Get(0).(*gorm.DB)
}

func TestNewJobService(t *testing.T) {
	jobService := NewJobService(&MockDatabase{})
	require.NotNil(t, jobService)
	assert.Implements(t, (*JobCreator)(nil), jobService)
}

func TestJobServiceCreate(t *testing.T) {
	ticket := NewTicket("dummy")

	mockDatabase := MockDatabase{}
	mockDatabase.On("Create", ticket).Return(&gorm.DB{})

	jobService := NewJobService(&mockDatabase)
	err := jobService.Create(ticket)

	assert.NoError(t, err)
	mockDatabase.AssertExpectations(t)
}

func TestJobServiceCreateError(t *testing.T) {
	ticket := NewTicket("dummy")
	simulatedErr := errors.New("simulated")

	mockDatabase := MockDatabase{}
	mockDatabase.On("Create", ticket).Return(&gorm.DB{Error: simulatedErr})

	jobService := NewJobService(&mockDatabase)
	err := jobService.Create(ticket)

	assert.Equal(t, simulatedErr, err)
	mockDatabase.AssertExpectations(t)
}
