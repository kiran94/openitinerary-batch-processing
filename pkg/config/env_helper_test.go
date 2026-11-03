package config

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestGetPositiveIntEnvOrDefault(t *testing.T) {
	tests := []struct {
		name          string
		value         string
		defaultValue  int
		expected      int
		expectedError string
	}{
		{name: "default", value: "", defaultValue: 10, expected: 10},
		{name: "environment override", value: "20", defaultValue: 10, expected: 20},
		{name: "malformed", value: "invalid", defaultValue: 10, expectedError: "TEST_POSITIVE_INT must be a positive integer"},
		{name: "zero", value: "0", defaultValue: 10, expectedError: "TEST_POSITIVE_INT must be a positive integer"},
		{name: "negative", value: "-1", defaultValue: 10, expectedError: "TEST_POSITIVE_INT must be a positive integer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_POSITIVE_INT", tt.value)

			actual, err := GetPositiveIntEnvOrDefault("TEST_POSITIVE_INT", tt.defaultValue)

			if tt.expectedError != "" {
				require.EqualError(t, err, tt.expectedError)
				assert.Zero(t, actual)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestGetPositiveInt64EnvOrDefault(t *testing.T) {
	tests := []struct {
		name          string
		value         string
		defaultValue  int64
		expected      int64
		expectedError string
	}{
		{name: "default", value: "", defaultValue: 10, expected: 10},
		{name: "environment override", value: "20", defaultValue: 10, expected: 20},
		{name: "malformed", value: "invalid", defaultValue: 10, expectedError: "TEST_POSITIVE_INT64 must be a positive integer"},
		{name: "zero", value: "0", defaultValue: 10, expectedError: "TEST_POSITIVE_INT64 must be a positive integer"},
		{name: "negative", value: "-1", defaultValue: 10, expectedError: "TEST_POSITIVE_INT64 must be a positive integer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TEST_POSITIVE_INT64", tt.value)

			actual, err := GetPositiveInt64EnvOrDefault("TEST_POSITIVE_INT64", tt.defaultValue)

			if tt.expectedError != "" {
				require.EqualError(t, err, tt.expectedError)
				assert.Zero(t, actual)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
