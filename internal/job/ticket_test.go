package job

import (
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
