package config

import (
	"fmt"
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
