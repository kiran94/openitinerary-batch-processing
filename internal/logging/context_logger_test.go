package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextLoggerAddsAvailableCorrelationValues(t *testing.T) {
	var output bytes.Buffer
	parent := newLogger(&output, "production", slog.LevelDebug)

	ctx := WithRequestID(t.Context(), "request-123")
	ctx = WithTicketID(ctx, "ticket-123")
	ctx = WithWorkerID(ctx, "worker-123")

	parent.WithContext(ctx).Info("ticket processed")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
	assert.Equal(t, "request-123", entry["request_id"])
	assert.Equal(t, "ticket-123", entry["ticket_id"])
	assert.Equal(t, "worker-123", entry["worker_id"])
}

func TestContextLoggerOmitsMissingCorrelationValues(t *testing.T) {
	var output bytes.Buffer
	parent := newLogger(&output, "production", slog.LevelDebug)

	ctx := WithTicketID(t.Context(), "ticket-123")
	parent.WithContext(ctx).Info("ticket processed")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
	assert.Equal(t, "ticket-123", entry["ticket_id"])
	assert.NotContains(t, entry, "request_id")
	assert.NotContains(t, entry, "worker_id")
}

func TestContextLoggerDoesNotModifyParentLogger(t *testing.T) {
	var output bytes.Buffer
	parent := newLogger(&output, "production", slog.LevelDebug)

	ctx := WithRequestID(t.Context(), "request-123")
	parent.WithContext(ctx).Info("child log")
	parent.Info("parent log")

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 2)

	var childEntry map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &childEntry))
	assert.Equal(t, "request-123", childEntry["request_id"])

	var parentEntry map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &parentEntry))
	assert.NotContains(t, parentEntry, "request_id")
}

func TestContextLoggerRetainsValuesAcrossLogs(t *testing.T) {
	var output bytes.Buffer
	parent := newLogger(&output, "production", slog.LevelDebug)

	ctx := WithWorkerID(t.Context(), "worker-123")
	child := parent.WithContext(ctx)
	child.Info("first log")
	child.Warn("second log")

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 2)
	for _, line := range lines {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		assert.Equal(t, "worker-123", entry["worker_id"])
	}
}

func TestContextLoggerNestedContextDoesNotDuplicateValues(t *testing.T) {
	var output bytes.Buffer
	parent := newLogger(&output, "production", slog.LevelDebug)

	ctx := WithRequestID(t.Context(), "request-123")
	ctx = WithTicketID(ctx, "ticket-123")

	child := parent.WithContext(ctx)
	child.WithContext(ctx).Info("nested log")

	raw := output.String()
	assert.Equal(t, 1, strings.Count(raw, `"request_id"`))
	assert.Equal(t, 1, strings.Count(raw, `"ticket_id"`))
}
