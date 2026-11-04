package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
)

func GetEnvOrDefault(name, defaultValue string) string {
	value := os.Getenv(name)
	if value == "" {
		value = defaultValue
	}

	return value
}

func GetEnvOrPanic(name string) string {
	if v := os.Getenv(name); v == "" {
		panic(fmt.Sprintf("env var %v must be defined", name))
	} else {
		return v
	}
}

// GetBoolOrDefault returns a native boolean type from the env var
// if the env var is not set then the default will be returned
// if the env var is set and the value is not understood as a boolean then an error is returned
func GetBoolOrDefault(name string, defaultValue bool) (bool, error) {
	v := os.Getenv(name)
	if v == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return defaultValue, fmt.Errorf("could not convert env var '%v': %w", name, err)
	}

	return parsed, nil
}

func GetPositiveIntEnvOrDefault(envVarName string, defaultValue int) (int, error) {
	value := os.Getenv(envVarName)
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, &ErrValueMustBePositiveInt{name: envVarName}
	}

	return parsed, nil
}

func GetPositiveInt64EnvOrDefault(envVarName string, defaultValue int64) (int64, error) {
	value := os.Getenv(envVarName)
	if value == "" {
		return defaultValue, nil
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return 0, &ErrValueMustBePositiveInt{name: envVarName}
	}

	return parsed, nil
}

// GetEnvLoggingLevelOrDefault gets a slog level from the environment or defaults to the passed value if the env var is not set
// it returns an error if the env var was set but that value cannot be parsed
func GetEnvLoggingLevelOrDefault(envName string, defaultValue slog.Level) (slog.Level, error) {
	raw := GetEnvOrDefault(envName, defaultValue.String())

	var levelParsed slog.Level
	if err := levelParsed.UnmarshalText([]byte(raw)); err != nil {
		return defaultValue, err
	}

	return levelParsed, nil
}

type ErrValueMustBePositiveInt struct {
	name string
}

func (e ErrValueMustBePositiveInt) Error() string {
	return fmt.Sprintf("%s must be a positive integer", e.name)
}
