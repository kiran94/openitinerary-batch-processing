package job

import (
	"errors"
	"testing"

	"github.com/google/uuid"
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

func (m *MockDatabase) Find(dest interface{}, conditions ...interface{}) (tx *gorm.DB) {
	mockArgs := append([]interface{}{dest}, conditions...)
	args := m.Called(mockArgs...)
	return args.Get(0).(*gorm.DB)
}

func TestNewJobService(t *testing.T) {
	jobService := NewJobService(&MockDatabase{})
	require.NotNil(t, jobService)
	assert.Implements(t, (*JobCreator)(nil), jobService)
}

func TestJobServiceCreate(t *testing.T) {
	ticket := NewTicket()

	mockDatabase := MockDatabase{}
	mockDatabase.On("Create", ticket).Return(&gorm.DB{})

	jobService := NewJobService(&mockDatabase)
	err := jobService.Create(ticket)

	require.NoError(t, err)
	mockDatabase.AssertExpectations(t)
}

func TestJobServiceCreateError(t *testing.T) {
	ticket := NewTicket()
	simulatedErr := errors.New("simulated")

	mockDatabase := MockDatabase{}
	mockDatabase.On("Create", ticket).Return(&gorm.DB{Error: simulatedErr})

	jobService := NewJobService(&mockDatabase)
	err := jobService.Create(ticket)

	assert.Equal(t, simulatedErr, err)
	mockDatabase.AssertExpectations(t)
}

func TestJobServiceGetError(t *testing.T) {

	id, _ := uuid.Parse("e6f26180-9b63-4799-815b-e67b3795bf53")
	expectedErr := errors.New("record not found")

	m := MockDatabase{}
	m.On("Find", mock.Anything, []string{id.String()}).Return(&gorm.DB{Error: expectedErr})

	job := NewJobService(&m)
	ticket, err := job.Get(id)

	m.AssertExpectations(t)
	assert.Equal(t, expectedErr, err)
	assert.Nil(t, ticket)
}

func TestJobServiceGetRecordNotFound(t *testing.T) {
	id, _ := uuid.Parse("e6f26180-9b63-4799-815b-e67b3795bf53")

	m := MockDatabase{}
	m.On("Find", mock.Anything, []string{id.String()}).Return(&gorm.DB{RowsAffected: 0})

	job := NewJobService(&m)
	ticket, err := job.Get(id)

	m.AssertExpectations(t)
	assert.Equal(t, ErrRecordNotFound, err)
	assert.Nil(t, ticket)
}

func TestJobServiceGet(t *testing.T) {
	id, _ := uuid.Parse("e6f26180-9b63-4799-815b-e67b3795bf53")

	expectedTicket := NewTicket()
	expectedTicket.DocumentPath = "example.pdf"

	m := MockDatabase{}
	m.On("Find", mock.AnythingOfType("*job.Ticket"), []string{id.String()}).Run(func(args mock.Arguments) {
		ticket := args.Get(0).(*Ticket)
		*ticket = *expectedTicket
	}).Return(&gorm.DB{RowsAffected: 1})

	job := NewJobService(&m)
	ticket, err := job.Get(id)

	require.NoError(t, err)
	assert.Equal(t, expectedTicket, ticket)
}
