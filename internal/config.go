package internal

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
	return GetEnvOrDefault("OIBP_BUCKET", "thebucketofkiran456")
}

func (ac ApplicationConfig) GetBucketPath() string {
	return GetEnvOrDefault("OIBP_BUCKET_PREFIX", "openitinerary")
}

func (ac ApplicationConfig) GetDocumentPath(bucket, bucketPath, jobId, fileType string) string {
	return "s3://" + path.Join(bucket, bucketPath, "jobs", jobId, "document"+"."+fileType)
}

func (ac ApplicationConfig) GetOutputPath(bucket, bucketPath, jobId string) string {
	return "s3://" + path.Join(bucket, bucketPath, "jobs", jobId, "result.json")
}

func GetEnvOrDefault(envVarName, defaultValue string) string {
	value := os.Getenv(envVarName)
	if value == "" {
		value = defaultValue
	}

	return value
}
