package config

import (
	"os"
	"path"
	"time"
)

type Config interface {
	GetPort() string

	GetServerReadTimeout() time.Duration
	GetServerWriteTimeout() time.Duration
	GetServerIdleTimeout() time.Duration

	GetMaximumAllowedRequestSizeMb() int64
	GetMaximumBufferMultipartForm() int64

	GetBucket() string
	GetBucketPath() string
	GetDocumentPath(bucket, bucketPath, jobId, fileType string) string
	GetOutputPath(bucket, bucketPath, jobId string) string

	GetQueueURL() string
	GetQueuePollingTime() int32
}

type ApplicationConfig struct{}

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

func (ac ApplicationConfig) GetBucket() string {
	return GetEnvOrDefault("OIBP_BUCKET", "openitinerary-batch-processing")
}

func (ac ApplicationConfig) GetBucketPath() string {
	return GetEnvOrDefault("OIBP_BUCKET_PREFIX", "")
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

func GetEnvOrDefault(envVarName, defaultValue string) string {
	value := os.Getenv(envVarName)
	if value == "" {
		value = defaultValue
	}

	return value
}
