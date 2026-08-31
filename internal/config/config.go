package config

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"time"
)

type Config interface {
	GetEnvironment() string
	GetPort() string

	GetServerReadTimeout() time.Duration
	GetServerWriteTimeout() time.Duration
	GetServerIdleTimeout() time.Duration

	GetMaximumAllowedRequestSizeMb() int64
	GetMaximumBufferMultipartForm() int64
	GetMaximumAdapterResponseSizeBytes() int64

	GetBucket() string
	GetBucketPath() string
	GetS3UploadConcurrency() int
	GetDocumentPath(bucket, bucketPath, jobId, fileType string) string
	GetOutputPath(bucket, bucketPath, jobId string) string

	GetQueueURL() string
	GetQueuePollingTime() int32

	GetWorkerTimeoutSeconds() int
}

type ApplicationConfig struct{}

func (ac ApplicationConfig) GetEnvironment() string {
	return os.Getenv("ENV")
}

func (ac ApplicationConfig) GetPort() string {
	return ":8080"
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

func (ac ApplicationConfig) GetDocumentPath(bucket, bucketPath, jobId, fileType string) string {
	return "s3://" + path.Join(bucket, bucketPath, "jobs", jobId, "document"+"."+fileType)
}

func (ac ApplicationConfig) GetOutputPath(bucket, bucketPath, jobId string) string {
	return "s3://" + path.Join(bucket, bucketPath, "jobs", jobId, "result.json")
}

func (ac ApplicationConfig) GetQueueURL() string {
	url := os.Getenv("SQS_QUEUE_URL")
	if url == "" {
		panic("SQS_QUEUE_URL must be set")
	}
	return url
}

func (ac ApplicationConfig) GetQueuePollingTime() int32 {
	return 20
}

func (ac ApplicationConfig) GetWorkerTimeoutSeconds() int {
	return getPositiveIntEnvOrDefault("OIBP_WORKER_TIMEOUT_SECONDS", 180)
}

func GetEnvOrDefault(envVarName, defaultValue string) string {
	value := os.Getenv(envVarName)
	if value == "" {
		value = defaultValue
	}

	return value
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
