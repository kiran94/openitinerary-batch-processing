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
	logger := newLogger(&output, "production", slog.LevelDebug)

	logger.Info("processed ticket", "ticket_id", "ticket-123")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
	assert.Equal(t, "INFO", entry["level"])
	assert.Equal(t, "processed ticket", entry["msg"])
	assert.Equal(t, "ticket-123", entry["ticket_id"])
	assert.NotContains(t, output.String(), "\x1b[") // ANSI color code
}

func TestNewLoggerUsesColoredTextOutsideProduction(t *testing.T) {
	var output bytes.Buffer
	logger := newLogger(&output, "development", slog.LevelDebug)

	logger.Debug("processed ticket", "ticket_id", "ticket-123")

	outputStr := output.String()
	assert.Contains(t, outputStr, "\x1b[") // ANSI color code
	assert.Contains(t, outputStr, "processed ticket")
	assert.Contains(t, outputStr, "ticket_id=")
	assert.Contains(t, outputStr, "ticket-123")
}

func TestSlogLoggerMethodsUseExpectedLevels(t *testing.T) {
	var output bytes.Buffer
	logger := &SlogLogger{
		logger: slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

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
