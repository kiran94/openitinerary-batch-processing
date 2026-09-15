package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLoggerUsesJSONInProduction(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(&output, "production", slog.LevelDebug, false)

	logger.Info(t.Context(), "processed ticket", "ticket_id", "ticket-123")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
	assert.Equal(t, "INFO", entry["level"])
	assert.Equal(t, "processed ticket", entry["msg"])
	assert.Equal(t, "ticket-123", entry["ticket_id"])
	assert.NotContains(t, output.String(), "\x1b[") // ANSI color code
}

func TestNewLoggerUsesColoredTextOutsideProduction(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(&output, "development", slog.LevelDebug, false)

	logger.Debug(t.Context(), "processed ticket", "ticket_id", "ticket-123")

	outputStr := output.String()
	assert.Contains(t, outputStr, "\x1b[") // ANSI color code
	assert.Contains(t, outputStr, "processed ticket")
	assert.Contains(t, outputStr, "ticket_id=")
	assert.Contains(t, outputStr, "ticket-123")
}

func TestMinimumLevelHandlerFiltersRecordsBelowConfiguredLevel(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(&minimumLevelHandler{
		level:   slog.LevelInfo,
		handler: slog.NewJSONHandler(&output, nil),
	})

	logger.DebugContext(t.Context(), "debug message")
	logger.InfoContext(t.Context(), "info message")

	assert.NotContains(t, output.String(), "debug message")
	assert.Contains(t, output.String(), "info message")
}

func TestMinimumLevelHandlerPreservesHandlerConfiguration(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(&minimumLevelHandler{
		level:   slog.LevelInfo,
		handler: slog.NewJSONHandler(&output, nil),
	}).WithGroup("context").With("component", "logging")

	logger.InfoContext(t.Context(), "info message")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
	assert.Equal(t, map[string]any{"component": "logging"}, entry["context"])
}

func TestSlogLoggerMethodsUseExpectedLevels(t *testing.T) {
	var output bytes.Buffer
	logger := &SlogLogger{
		logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}

	logger.Debug(t.Context(), "debug message")
	logger.Info(t.Context(), "info message")
	logger.Warn(t.Context(), "warn message")
	logger.Error(t.Context(), "error message")

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	require.Len(t, lines, 4)

	levels := make([]string, 0, len(lines))
	for _, line := range lines {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		levels = append(levels, entry["level"].(string))
	}

	assert.Equal(t, []string{"DEBUG", "INFO", "WARN", "ERROR"}, levels)
}

func TestNewLogger(t *testing.T) {
	logger := New(config.ApplicationConfig{})
	assert.NotNil(t, logger)
}

func TestContextValuesTogether(t *testing.T) {
	ctx := t.Context()

	updatedCtx := WithRequestID(ctx, "rid")
	updatedCtx = WithTicketID(updatedCtx, "tid")
	updatedCtx = WithWorkerID(updatedCtx, "wid")

	rid, ridOk := GetRequestID(updatedCtx)
	tid, tidOk := GetTicketID(updatedCtx)
	wid, widOk := GetWorkerID(updatedCtx)

	assert.True(t, ridOk)
	assert.True(t, widOk)
	assert.True(t, tidOk)

	assert.Equal(t, "rid", rid)
	assert.Equal(t, "wid", wid)
	assert.Equal(t, "tid", tid)
}

func TestContextRequestID(t *testing.T) {
	ctx := t.Context()
	inputValue := "123"

	updatedCtx := WithRequestID(ctx, inputValue)

	v, ok := GetRequestID(updatedCtx)

	assert.True(t, ok)
	assert.Equal(t, "123", v)
}

func TestContextTicketID(t *testing.T) {
	ctx := t.Context()
	inputValue := "123"

	updatedCtx := WithTicketID(ctx, inputValue)

	v, ok := GetTicketID(updatedCtx)

	assert.True(t, ok)
	assert.Equal(t, "123", v)
}

func TestContextWorkerID(t *testing.T) {
	ctx := t.Context()
	inputValue := "123"

	updatedCtx := WithWorkerID(ctx, inputValue)

	v, ok := GetWorkerID(updatedCtx)

	assert.True(t, ok)
	assert.Equal(t, "123", v)
}

func TestMissingContextRequestID(t *testing.T) {
	_, ok := GetRequestID(t.Context())
	assert.False(t, ok)
}

func TestMissingContextTicketID(t *testing.T) {
	_, ok := GetTicketID(t.Context())
	assert.False(t, ok)
}

func TestMissingContextWorkerID(t *testing.T) {
	_, ok := GetWorkerID(t.Context())
	assert.False(t, ok)
}

func TestComponentLogValuer(t *testing.T) {
	value := slog.AnyValue(ComponentDatabase).Resolve()

	assert.Equal(t, slog.KindString, value.Kind())
	assert.Equal(t, string(ComponentDatabase), value.String())
}

func TestOperationLogValuer(t *testing.T) {
	value := slog.AnyValue(OperationSubmit).Resolve()

	assert.Equal(t, slog.KindString, value.Kind())
	assert.Equal(t, string(OperationSubmit), value.String())
}
