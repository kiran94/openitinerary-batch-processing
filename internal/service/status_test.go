package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/job"
	"github.com/kiran94/openitinerary-batch-processing/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGetStatusSuccess(t *testing.T) {
	ticketID := uuid.New()
	expectedTicket := job.NewTicket()
	expectedTicket.ID = ticketID
	jobs := new(statusTicketRepositoryMock)
	jobs.
		On("Get", t.Context(), ticketID).
		Return(expectedTicket, nil).
		Once()
	statusService := NewStatusService(jobs, testutils.NewMockTracer())

	ticket, err := statusService.GetStatus(t.Context(), ticketID)

	require.NoError(t, err)
	assert.Same(t, expectedTicket, ticket)

	jobs.AssertExpectations(t)
}

func TestGetStatusTicketLookupError(t *testing.T) {
	tests := []struct {
		name           string
		ticketGetError error
	}{
		{name: "ticket not found", ticketGetError: job.ErrRecordNotFound},
		{name: "ticket lookup failed", ticketGetError: errors.New("failed to get ticket")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ticketID := uuid.New()
			jobs := new(statusTicketRepositoryMock)
			jobs.
				On("Get", t.Context(), ticketID).
				Return(nil, tt.ticketGetError).
				Once()
			statusService := NewStatusService(jobs, testutils.NewMockTracer())

			ticket, err := statusService.GetStatus(t.Context(), ticketID)

			require.ErrorIs(t, err, tt.ticketGetError)
			assert.Nil(t, ticket)

			jobs.AssertExpectations(t)
		})
	}
}

type statusTicketRepositoryMock struct {
	job.TicketRepository
	mock.Mock
}

func (m *statusTicketRepositoryMock) Get(ctx context.Context, id uuid.UUID) (*job.Ticket, error) {
	args := m.Called(ctx, id)
	if ticket := args.Get(0); ticket != nil {
		return ticket.(*job.Ticket), args.Error(1)
	}
	return nil, args.Error(1)
}

var _ job.TicketRepository = (*statusTicketRepositoryMock)(nil)
