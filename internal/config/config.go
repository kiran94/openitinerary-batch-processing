package config

import (
	"log/slog"
	"os"
	"path"
	"time"

	env "github.com/kiran94/openitinerary-batch-processing/pkg/config"
)

type Config interface {
	GetEnvironment() string
	GetPort() string
	GetLoggingLevel() slog.Level
	GetDatabaseConnection() string

	GetServerReadTimeout() time.Duration
	GetServerWriteTimeout() time.Duration
	GetServerIdleTimeout() time.Duration

	GetGrpcServerIdleTimeout() time.Duration
	GetGrpcServerMaxConnectionAge() time.Duration
	GetGRPCUploadTimeout() time.Duration

	GetMaximumAllowedRequestSizeMb() int64
	GetMaximumBufferMultipartForm() int64
	GetMaximumAdapterResponseSizeBytes() int64

	GetBucket() string
	GetBucketPath() string
	GetS3UploadConcurrency() int
	GetS3PresignedExpirationSeconds() int
	GetS3UsePathStyle() bool
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
	GetOtelGrpcName() string
	GetOtelHandlerName() string

	GetServiceReadinessPollingInterval() time.Duration
	GetServiceReadinessPollingTimeout() time.Duration
}

type ApplicationConfig struct{}

func NewConfig() Config {
	return ApplicationConfig{}
}

func (ac ApplicationConfig) GetEnvironment() string {
	return os.Getenv("ENV")
}

func (ac ApplicationConfig) GetPort() string {
	return ":8080"
}

func (ac ApplicationConfig) GetDatabaseConnection() string {
	return env.GetEnvOrDefault(
		"DATABASE_DSN",
		"host=localhost user=openitinerary password=openitinerary dbname=openitinerary_batch_processing port=5432 sslmode=disable",
	)
}

func (ac ApplicationConfig) GetLoggingLevel() slog.Level {
	rawLevel := env.GetEnvOrDefault("OIBP_LOG_LEVEL", slog.LevelInfo.String())

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

func (ac ApplicationConfig) GetGrpcServerIdleTimeout() time.Duration {
	return time.Second * 30

}

func (ac ApplicationConfig) GetGrpcServerMaxConnectionAge() time.Duration {
	return time.Minute * 5
}

func (ac ApplicationConfig) GetGRPCUploadTimeout() time.Duration {
	return time.Minute * 30
}

func (ac ApplicationConfig) GetMaximumAllowedRequestSizeMb() int64 {
	return 10 << 20 // 10MB
}

func (ac ApplicationConfig) GetMaximumBufferMultipartForm() int64 {
	return 5 << 20 // 5MB
}

func (ac ApplicationConfig) GetMaximumAdapterResponseSizeBytes() int64 {
	return env.GetPositiveInt64EnvOrDefault("OIBP_MAX_ADAPTER_RESPONSE_SIZE_BYTES", 25<<20)
}

func (ac ApplicationConfig) GetBucket() string {
	return env.GetEnvOrDefault("OIBP_BUCKET", "openitinerary-batch-processing")
}

func (ac ApplicationConfig) GetBucketPath() string {
	return env.GetEnvOrDefault("OIBP_BUCKET_PREFIX", "")
}

func (ac ApplicationConfig) GetS3UploadConcurrency() int {
	return env.GetPositiveIntEnvOrDefault("OIBP_S3_UPLOAD_CONCURRENCY", 1)
}

func (ac ApplicationConfig) GetS3PresignedExpirationSeconds() int {
	v := env.GetPositiveIntEnvOrDefault("OIBP_S3_PRESIGNED_EXPIRE_SECONDS", 3600)

	// https://docs.aws.amazon.com/AmazonS3/latest/developerguide/sigv4-query-string-auth.html
	if v > 604800 {
		panic("OIBP_S3_PRESIGNED_EXPIRE_SECONDS exceeded the maximum allowed by AWS")
	}

	return v
}

func (ac ApplicationConfig) GetS3UsePathStyle() bool {
	return env.GetBoolOrDefault("OIBP_S3_USE_PATH_STYLE", false)
}

func (ac ApplicationConfig) GetDocumentPath(bucket, bucketPath, jobId, fileType string) string {
	return "s3://" + path.Join(bucket, bucketPath, "jobs", jobId, "document"+"."+fileType)
}

func (ac ApplicationConfig) GetOutputPath(bucket, bucketPath, jobId string) string {
	return "s3://" + path.Join(bucket, bucketPath, "jobs", jobId, "result.json")
}

func (ac ApplicationConfig) GetQueueURL() string {
	return env.GetEnvOrPanic("SQS_QUEUE_URL")
}

func (ac ApplicationConfig) GetQueuePollingTime() int32 {
	return 20
}

func (ac ApplicationConfig) GetWorkerTimeoutSeconds() int {
	return env.GetPositiveIntEnvOrDefault("OIBP_WORKER_TIMEOUT_SECONDS", 300)
}

func (ac ApplicationConfig) GetSESConfigurationSetName() string {
	return env.GetEnvOrPanic("SES_NOTIFICATION_CONFIGURATION_SET_NAME")
}

func (ac ApplicationConfig) GetSESAddressFrom() string {
	return env.GetEnvOrPanic("SES_NOTIFICATION_ADDRESS_FROM")
}

func (ac ApplicationConfig) GetNotificationEmailLimit() int {
	return env.GetPositiveIntEnvOrDefault("OIBP_NOTIFICATION_EMAIL_LIMIT", 5)
}

func (ac ApplicationConfig) GetOtelServiceName() string {
	return "openitinerary-batch-api"
}

func (ac ApplicationConfig) GetOtelWorkerName() string {
	return "openitinerary-batch-worker"
}

func (ac ApplicationConfig) GetOtelGrpcName() string {
	return "openitinerary-batch-grpc"
}

func (ac ApplicationConfig) GetOtelHandlerName() string {
	return "github.com/kiran94/openitinerary-batch-processing/internal/logging"
}

// This value is injected via the build process
// See the Makefile for more details
var Version = "dev"

func (ac ApplicationConfig) GetOtelServiceVersion() string {
	return Version
}

func (ac ApplicationConfig) GetOtelEnabled() bool {
	return env.GetBoolOrDefault("OIBP_TELEMETRY_ENABLED", true)
}

func (ac ApplicationConfig) GetServiceReadinessPollingInterval() time.Duration {
	return 5 * time.Second
}

func (ac ApplicationConfig) GetServiceReadinessPollingTimeout() time.Duration {
	return 2 * time.Second
}
