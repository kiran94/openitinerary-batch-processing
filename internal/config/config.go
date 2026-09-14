package config

import (
	"fmt"
	"log/slog"
	"os"
	"path"
	"strconv"
	"time"
)

type Config interface {
	GetEnvironment() string
	GetPort() string
	GetLoggingLevel() slog.Level
	GetDatabaseConnection() string

	GetServerReadTimeout() time.Duration
	GetServerWriteTimeout() time.Duration
	GetServerIdleTimeout() time.Duration

	GetMaximumAllowedRequestSizeMb() int64
	GetMaximumBufferMultipartForm() int64
	GetMaximumAdapterResponseSizeBytes() int64

	GetBucket() string
	GetBucketPath() string
	GetS3UploadConcurrency() int
	GetS3PresignedExpirationSeconds() int
	GetDocumentPath(bucket, bucketPath, jobId, fileType string) string
	GetOutputPath(bucket, bucketPath, jobId string) string

	GetQueueURL() string
	GetQueuePollingTime() int32

	GetSESConfigurationSetName() string
	GetSESAddressFrom() string
	GetNotificationEmailLimit() int

	GetWorkerTimeoutSeconds() int

	GetOtelEnabled() bool
	GetOtelServiceName() string
	GetOtelWorkerName() string
	GetOtelServiceVersion() string

	GetServiceReadinessPollingInterval() time.Duration
	GetServiceReadinessPollingTimeout() time.Duration
}

type ApplicationConfig struct{}

func (ac ApplicationConfig) GetEnvironment() string {
	return os.Getenv("ENV")
}

func (ac ApplicationConfig) GetPort() string {
	return ":8080"
}

func (ac ApplicationConfig) GetDatabaseConnection() string {
	return GetEnvOrDefault(
		"DATABASE_DSN",
		"host=localhost user=openitinerary password=openitinerary dbname=openitinerary_batch_processing port=5432 sslmode=disable",
	)
}

func (ac ApplicationConfig) GetLoggingLevel() slog.Level {
	rawLevel := GetEnvOrDefault("OIBP_LOG_LEVEL", slog.LevelInfo.String())

	levelParsed := slog.LevelInfo
	if err := levelParsed.UnmarshalText([]byte(rawLevel)); err != nil {
		panic(err)
	}

	return levelParsed
}

func (ac ApplicationConfig) GetServerReadTimeout() time.Duration {
	return 30 * time.Second
}

func (ac ApplicationConfig) GetServerWriteTimeout() time.Duration {
	return 30 * time.Second
}

func (ac ApplicationConfig) GetServerIdleTimeout() time.Duration {
	return 15 * time.Second
}

func (ac ApplicationConfig) GetMaximumAllowedRequestSizeMb() int64 {
	return 10 << 20 // 10MB
}

func (ac ApplicationConfig) GetMaximumBufferMultipartForm() int64 {
	return 5 << 20 // 5MB
}

func (ac ApplicationConfig) GetMaximumAdapterResponseSizeBytes() int64 {
	return getPositiveInt64EnvOrDefault("OIBP_MAX_ADAPTER_RESPONSE_SIZE_BYTES", 25<<20)
}

func (ac ApplicationConfig) GetBucket() string {
	return GetEnvOrDefault("OIBP_BUCKET", "openitinerary-batch-processing")
}

func (ac ApplicationConfig) GetBucketPath() string {
	return GetEnvOrDefault("OIBP_BUCKET_PREFIX", "")
}

func (ac ApplicationConfig) GetS3UploadConcurrency() int {
	return getPositiveIntEnvOrDefault("OIBP_S3_UPLOAD_CONCURRENCY", 1)
}

func (ac ApplicationConfig) GetS3PresignedExpirationSeconds() int {
	v := getPositiveIntEnvOrDefault("OIBP_S3_PRESIGNED_EXPIRE_SECONDS", 3600)

	// https://docs.aws.amazon.com/AmazonS3/latest/developerguide/sigv4-query-string-auth.html
	if v > 604800 {
		panic("OIBP_S3_PRESIGNED_EXPIRE_SECONDS exceeded the maximum allowed by AWS")
	}

	return v
}

func (ac ApplicationConfig) GetDocumentPath(bucket, bucketPath, jobId, fileType string) string {
	return "s3://" + path.Join(bucket, bucketPath, "jobs", jobId, "document"+"."+fileType)
}

func (ac ApplicationConfig) GetOutputPath(bucket, bucketPath, jobId string) string {
	return "s3://" + path.Join(bucket, bucketPath, "jobs", jobId, "result.json")
}

func (ac ApplicationConfig) GetQueueURL() string {
	return GetEnvOrPanic("SQS_QUEUE_URL")
}

func (ac ApplicationConfig) GetQueuePollingTime() int32 {
	return 20
}

func (ac ApplicationConfig) GetWorkerTimeoutSeconds() int {
	return getPositiveIntEnvOrDefault("OIBP_WORKER_TIMEOUT_SECONDS", 300)
}

func (ac ApplicationConfig) GetSESConfigurationSetName() string {
	return GetEnvOrPanic("SES_NOTIFICATION_CONFIGURATION_SET_NAME")
}

func (ac ApplicationConfig) GetSESAddressFrom() string {
	return GetEnvOrPanic("SES_NOTIFICATION_ADDRESS_FROM")
}

func (ac ApplicationConfig) GetNotificationEmailLimit() int {
	return getPositiveIntEnvOrDefault("OIBP_NOTIFICATION_EMAIL_LIMIT", 5)
}

func (ac ApplicationConfig) GetOtelServiceName() string {
	return "openitinerary-batch-api"
}

func (ac ApplicationConfig) GetOtelWorkerName() string {
	return "openitinerary-batch-worker"
}

func (ac ApplicationConfig) GetOtelServiceVersion() string {
	return "0.0.1"
}

func (ac ApplicationConfig) GetOtelEnabled() bool {
	return getBoolOrDefault("OIBP_TELEMETRY_ENABLED", true)
}

func (ac ApplicationConfig) GetServiceReadinessPollingInterval() time.Duration {
	return 5 * time.Second
}

func (ac ApplicationConfig) GetServiceReadinessPollingTimeout() time.Duration {
	return 5 * time.Second

}

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

func getBoolOrDefault(name string, defaultValue bool) bool {
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

func getPositiveIntEnvOrDefault(envVarName string, defaultValue int) int {
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

func getPositiveInt64EnvOrDefault(envVarName string, defaultValue int64) int64 {
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
