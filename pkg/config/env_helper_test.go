package config

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

const envVarName = "TEST_OIBP_LOGGING_LEVEL"

func TestGetEnvLoggingLevelOrDefault_EnvNotSet_ExpectDefault(t *testing.T) {
	t.Setenv(envVarName, "")
	defaultValue := slog.LevelInfo
	level := GetEnvLoggingLevelOrDefault(envVarName, defaultValue)

	assert.Equal(t, slog.LevelInfo, level)
}

func TestGetEnvLoggingLevelOrDefault_EnvSet_ExpectDefault(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{input: "DEBUG", expected: slog.LevelDebug},
		{input: "INFO", expected: slog.LevelInfo},
		{input: "WARN", expected: slog.LevelWarn},
		{input: "ERROR", expected: slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Setenv(envVarName, tt.input)
			level := GetEnvLoggingLevelOrDefault(envVarName, slog.LevelInfo)

			assert.Equal(t, tt.expected, level)
		})
	}
}

func TestGetEnvLoggingLevelOrDefault_EnvSet_Malformed(t *testing.T) {
	t.Setenv(envVarName, "NOT_LOGGING_LEVEL")
	assert.Panics(t, func() {
		GetEnvLoggingLevelOrDefault(envVarName, slog.LevelInfo)
	})
}
