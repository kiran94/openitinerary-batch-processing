package job

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewTicket(t *testing.T) {
	documentPath := "document_path"

	ticket := NewTicket()
	ticket.DocumentPath = documentPath

	assert.NotNil(t, ticket.ID)
	assert.Equal(t, documentPath, ticket.DocumentPath)
	assert.Equal(t, Queued, ticket.Status)
}

func TestUpdateStatus(t *testing.T) {
	tests := []struct {
		name                 string
		ticket               Ticket
		status               TicketStatus
		statusReason         *TicketStatusReason
		err                  error
		expectedStatus       TicketStatus
		expectedStatusReason *TicketStatusReason
		expectedError        *string
	}{
		{
			name:           "queued_to_processing",
			ticket:         Ticket{Status: Queued},
			status:         Processing,
			expectedStatus: Processing,
		},
		{
			name:                 "processing_to_failed",
			ticket:               Ticket{Status: Processing},
			status:               Failed,
			statusReason:         new(ConvertFailed),
			err:                  errors.New("convert failed"),
			expectedStatus:       Failed,
			expectedStatusReason: new(ConvertFailed),
			expectedError:        new("convert failed"),
		},
		{
			name:           "failed_to_processing",
			ticket:         Ticket{Status: Failed, StatusReason: new(ConvertFailed), Error: new("convert failed")},
			status:         Processing,
			expectedStatus: Processing,
		},
		{
			name:           "processing_to_complete",
			ticket:         Ticket{Status: Processing, StatusReason: new(ConvertFailed), Error: new("convert failed")},
			status:         Complete,
			expectedStatus: Complete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ticket := tt.ticket
			ticket.UpdateStatus(tt.status, tt.statusReason, tt.err)

			assert.Equal(t, tt.expectedStatus, ticket.Status)
			assert.Equal(t, tt.expectedStatusReason, ticket.StatusReason)
			assert.Equal(t, tt.expectedError, ticket.Error)
		})
	}
}
