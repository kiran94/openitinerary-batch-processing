package job

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type MockDatabase struct {
	mock.Mock
}

func newTestTicketRepository(database Database) TicketRepository {
	return NewTicketRepository(database, testutils.NewMockTracer())
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

func (m *MockDatabase) WithContext(ctx context.Context) *gorm.DB {
	return m.Called(ctx).Get(0).(*gorm.DB)
}

func TestNewTicketRepository(t *testing.T) {
	repository := newTestTicketRepository(&MockDatabase{})
	require.NotNil(t, repository)
	assert.Implements(t, (*TicketRepository)(nil), repository)
}

func TestTicketRepositoryCreate(t *testing.T) {
	db := newInMemoryDatabase(t)
	ticket := NewTicket()
	repository := newTestTicketRepository(db)

	err := repository.Create(t.Context(), ticket)

	require.NoError(t, err)

	var createdTicket Ticket
	require.NoError(t, db.First(&createdTicket, "id = ?", ticket.ID).Error)
	assert.Equal(t, ticket.ID, createdTicket.ID)
}

func TestTicketRepositoryCreateError(t *testing.T) {
	db := newInMemoryDatabase(t)
	require.NoError(t, db.Migrator().DropTable(&Ticket{}))
	ticket := NewTicket()
	repository := newTestTicketRepository(db)

	err := repository.Create(t.Context(), ticket)

	require.Error(t, err)
}

func TestTicketRepositoryCreateCanceled(t *testing.T) {
	db := newInMemoryDatabase(t)
	repository := newTestTicketRepository(db)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := repository.Create(ctx, NewTicket())

	require.ErrorIs(t, err, context.Canceled)

	var ticketCount int64
	require.NoError(t, db.Model(&Ticket{}).Count(&ticketCount).Error)
	assert.Zero(t, ticketCount)
}

func TestTicketRepositoryMarkFailedEnqueueFailed(t *testing.T) {
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	detail := "enqueue failed. err: simulated"

	db := newInMemoryDatabase(t)
	ticket := NewTicket()
	ticket.ID = id
	require.NoError(t, db.Create(ticket).Error)

	repository := newTestTicketRepository(db)
	updated, err := repository.MarkFailed(t.Context(), id, EnqueueFailed, detail)

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

func TestTicketRepositoryMarkFailedEnqueueFailedStatusChanged(t *testing.T) {
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	detail := "enqueue failed. err: simulated"

	db := newInMemoryDatabase(t)
	ticket := NewTicket()
	ticket.ID = id
	// Simulate a worker claiming the ticket before enqueue reports an error.
	ticket.Status = Processing
	require.NoError(t, db.Create(ticket).Error)

	repository := newTestTicketRepository(db)
	updated, err := repository.MarkFailed(t.Context(), id, EnqueueFailed, detail)

	require.NoError(t, err)
	assert.False(t, updated)

	var unchangedTicket Ticket
	require.NoError(t, db.First(&unchangedTicket, "id = ?", id).Error)
	assert.Equal(t, Processing, unchangedTicket.Status)
	assert.Nil(t, unchangedTicket.StatusReason)
	assert.Nil(t, unchangedTicket.Error)
}

func TestTicketRepositoryMarkFailedEnqueueFailedError(t *testing.T) {
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	detail := "enqueue failed. err: simulated"
	db := newInMemoryDatabase(t)
	// Remove the schema to make the conditional update return a database error.
	require.NoError(t, db.Migrator().DropTable(&Ticket{}))

	repository := newTestTicketRepository(db)
	updated, err := repository.MarkFailed(t.Context(), id, EnqueueFailed, detail)

	require.Error(t, err)
	assert.False(t, updated)
}

func TestTicketRepositoryMarkFailedCanceled(t *testing.T) {
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	db := newInMemoryDatabase(t)
	ticket := NewTicket()
	ticket.ID = id
	require.NoError(t, db.Create(ticket).Error)
	repository := newTestTicketRepository(db)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	updated, err := repository.MarkFailed(ctx, id, EnqueueFailed, "enqueue failed")

	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, updated)

	var unchangedTicket Ticket
	require.NoError(t, db.First(&unchangedTicket, "id = ?", id).Error)
	assert.Equal(t, Queued, unchangedTicket.Status)
	assert.Nil(t, unchangedTicket.StatusReason)
	assert.Nil(t, unchangedTicket.Error)
}

func TestTicketRepositoryMarkFailedUnsupportedReason(t *testing.T) {
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	// Use an unregistered reason to verify it is rejected before database access.
	reason := TicketStatusReason("unsupported")

	m := MockDatabase{}
	repository := newTestTicketRepository(&m)
	updated, err := repository.MarkFailed(t.Context(), id, reason, "simulated")

	require.ErrorIs(t, err, ErrUnsupportedTicketStatusReason)
	assert.False(t, updated)
	m.AssertNotCalled(t, "WithContext", mock.Anything)
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

func TestTicketRepositoryGetError(t *testing.T) {
	db := newInMemoryDatabase(t)
	require.NoError(t, db.Migrator().DropTable(&Ticket{}))
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	repository := newTestTicketRepository(db)

	ticket, err := repository.Get(t.Context(), id)

	require.Error(t, err)
	assert.Nil(t, ticket)
}

func TestTicketRepositoryGetRecordNotFound(t *testing.T) {
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	repository := newTestTicketRepository(newInMemoryDatabase(t))

	ticket, err := repository.Get(t.Context(), id)

	require.ErrorIs(t, err, ErrRecordNotFound)
	assert.Nil(t, ticket)
}

func TestTicketRepositoryGet(t *testing.T) {
	db := newInMemoryDatabase(t)
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	expectedTicket := NewTicket()
	expectedTicket.ID = id
	expectedTicket.DocumentPath = "example.pdf"
	require.NoError(t, db.Create(expectedTicket).Error)
	repository := newTestTicketRepository(db)

	ticket, err := repository.Get(t.Context(), id)

	require.NoError(t, err)
	assert.Equal(t, expectedTicket, ticket)
}

func TestTicketRepositoryGetCanceled(t *testing.T) {
	repository := newTestTicketRepository(newInMemoryDatabase(t))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	ticket, err := repository.Get(ctx, uuid.New())

	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, ticket)
}

func TestTicketRepositoryUpdate(t *testing.T) {
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	db := newInMemoryDatabase(t)
	ticket := NewTicket()
	ticket.ID = id
	require.NoError(t, db.Create(ticket).Error)

	ticket.Status = Processing
	repository := newTestTicketRepository(db)
	require.NoError(t, repository.Update(t.Context(), ticket))

	var updatedTicket Ticket
	require.NoError(t, db.First(&updatedTicket, "id = ?", id).Error)
	assert.Equal(t, Processing, updatedTicket.Status)
}

func TestTicketRepositoryUpdateRecordNotFound(t *testing.T) {
	repository := newTestTicketRepository(newInMemoryDatabase(t))

	err := repository.Update(t.Context(), NewTicket())

	require.ErrorIs(t, err, ErrRecordNotFound)
}

func TestTicketRepositoryUpdateError(t *testing.T) {
	db := newInMemoryDatabase(t)
	require.NoError(t, db.Migrator().DropTable(&Ticket{}))

	repository := newTestTicketRepository(db)
	err := repository.Update(t.Context(), NewTicket())

	require.Error(t, err)
}

func TestTicketRepositoryUpdateCanceled(t *testing.T) {
	id := uuid.MustParse("e6f26180-9b63-4799-815b-e67b3795bf53")
	db := newInMemoryDatabase(t)
	ticket := NewTicket()
	ticket.ID = id
	require.NoError(t, db.Create(ticket).Error)
	repository := newTestTicketRepository(db)
	ticket.Status = Processing
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := repository.Update(ctx, ticket)

	require.ErrorIs(t, err, context.Canceled)

	var unchangedTicket Ticket
	require.NoError(t, db.First(&unchangedTicket, "id = ?", id).Error)
	assert.Equal(t, Queued, unchangedTicket.Status)
}
