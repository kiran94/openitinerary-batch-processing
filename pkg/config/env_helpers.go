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

func GetBoolOrDefault(name string, defaultValue bool) bool {
	v := os.Getenv(name)
	if v == "" {
		return defaultValue
	}

	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return defaultValue
	}

	return parsed
}

func GetPositiveIntEnvOrDefault(envVarName string, defaultValue int) int {
	value := os.Getenv(envVarName)
	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		panic(fmt.Sprintf("%s must be a positive integer", envVarName))
	}

	return parsed
}

func GetPositiveInt64EnvOrDefault(envVarName string, defaultValue int64) int64 {
	value := os.Getenv(envVarName)
	if value == "" {
		return defaultValue
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		panic(fmt.Sprintf("%s must be a positive integer", envVarName))
	}

	return parsed
}

// GetEnvLoggingLevelOrDefault gets a slog level from the environment or defaults to the passed value if the env var is not set
// It will only panic if the env variable is actually set but is marlformed. This is so we notify the developer that there is a misconfiguration in the environment early.
func GetEnvLoggingLevelOrDefault(envName string, defaultValue slog.Level) slog.Level {
	raw := GetEnvOrDefault(envName, defaultValue.String())

	var levelParsed slog.Level
	if err := levelParsed.UnmarshalText([]byte(raw)); err != nil {
		panic(err)
	}

	return levelParsed
}
