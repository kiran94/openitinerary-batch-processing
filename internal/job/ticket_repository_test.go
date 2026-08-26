package job

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
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

func (m *MockDatabase) Model(value interface{}) (tx *gorm.DB) {
	return m.Called(value).Get(0).(*gorm.DB).Model(value)
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

func TestJobServiceMarkFailedEnqueueFailed(t *testing.T) {
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	detail := "enqueue failed. err: simulated"

	db := newInMemoryDatabase(t)
	ticket := NewTicket()
	ticket.ID = id
	require.NoError(t, db.Create(ticket).Error)

	jobService := NewJobService(db)
	updated, err := jobService.MarkFailed(id, EnqueueFailed, detail)

	require.NoError(t, err)
	assert.True(t, updated)

	var updatedTicket Ticket
	require.NoError(t, db.First(&updatedTicket, "id = ?", id).Error)

	require.NotNil(t, updatedTicket.StatusReason)
	require.NotNil(t, updatedTicket.Error)

	assert.Equal(t, EnqueueFailed, *updatedTicket.StatusReason)
	assert.Equal(t, detail, *updatedTicket.Error)
	assert.Equal(t, Failed, updatedTicket.Status)
}

func TestJobServiceMarkFailedEnqueueFailedStatusChanged(t *testing.T) {
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	detail := "enqueue failed. err: simulated"

	db := newInMemoryDatabase(t)
	ticket := NewTicket()
	ticket.ID = id
	// Simulate a worker claiming the ticket before enqueue reports an error.
	ticket.Status = Processing
	require.NoError(t, db.Create(ticket).Error)

	jobService := NewJobService(db)
	updated, err := jobService.MarkFailed(id, EnqueueFailed, detail)

	require.NoError(t, err)
	assert.False(t, updated)

	var unchangedTicket Ticket
	require.NoError(t, db.First(&unchangedTicket, "id = ?", id).Error)
	assert.Equal(t, Processing, unchangedTicket.Status)
	assert.Nil(t, unchangedTicket.StatusReason)
	assert.Nil(t, unchangedTicket.Error)
}

func TestJobServiceMarkFailedEnqueueFailedError(t *testing.T) {
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	detail := "enqueue failed. err: simulated"
	db := newInMemoryDatabase(t)
	// Remove the schema to make the conditional update return a database error.
	require.NoError(t, db.Migrator().DropTable(&Ticket{}))

	jobService := NewJobService(db)
	updated, err := jobService.MarkFailed(id, EnqueueFailed, detail)

	require.Error(t, err)
	assert.False(t, updated)
}

func TestJobServiceMarkFailedUnsupportedReason(t *testing.T) {
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	// Use an unregistered reason to verify it is rejected before database access.
	reason := TicketStatusReason("unsupported")

	m := MockDatabase{}
	jobService := NewJobService(&m)
	updated, err := jobService.MarkFailed(id, reason, "simulated")

	require.ErrorIs(t, err, ErrUnsupportedTicketStatusReason)
	assert.False(t, updated)
	m.AssertNotCalled(t, "Model", mock.Anything)
}

func newInMemoryDatabase(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := "file:" + uuid.NewString() + "?mode=memory&cache=shared"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Ticket{}))

	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, sqlDB.Close())
	})

	return db
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
