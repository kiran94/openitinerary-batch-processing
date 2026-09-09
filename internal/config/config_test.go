package config

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestApplicationConfigGetEnvironment(t *testing.T) {
	t.Setenv("ENV", "production")
	config := ApplicationConfig{}

	assert.Equal(t, "production", config.GetEnvironment())
}

func TestApplicationConfigGetEnvironmentUnset(t *testing.T) {
	t.Setenv("ENV", "")
	config := ApplicationConfig{}

	assert.Empty(t, config.GetEnvironment())
}

func TestApplicationConfigServerDefaults(t *testing.T) {
	config := ApplicationConfig{}

	assert.Equal(t, ":8080", config.GetPort())
	assert.Equal(t, 30*time.Second, config.GetServerReadTimeout())
	assert.Equal(t, 30*time.Second, config.GetServerWriteTimeout())
	assert.Equal(t, 15*time.Second, config.GetServerIdleTimeout())
}

func TestApplicationConfigUploadDefaults(t *testing.T) {
	t.Setenv("OIBP_MAX_ADAPTER_RESPONSE_SIZE_BYTES", "")
	config := ApplicationConfig{}

	assert.Equal(t, int64(10<<20), config.GetMaximumAllowedRequestSizeMb())
	assert.Equal(t, int64(5<<20), config.GetMaximumBufferMultipartForm())
	assert.Equal(t, int64(25<<20), config.GetMaximumAdapterResponseSizeBytes())
}

func TestApplicationConfigStorageDefaults(t *testing.T) {
	t.Setenv("OIBP_BUCKET", "")
	t.Setenv("OIBP_BUCKET_PREFIX", "")
	t.Setenv("OIBP_S3_UPLOAD_CONCURRENCY", "")
	config := ApplicationConfig{}

	assert.Equal(t, "openitinerary-batch-processing", config.GetBucket())
	assert.Empty(t, config.GetBucketPath())
	assert.Equal(t, 1, config.GetS3UploadConcurrency())
}

func TestApplicationConfigStorageEnvironmentOverrides(t *testing.T) {
	t.Setenv("OIBP_BUCKET", "test-bucket")
	t.Setenv("OIBP_BUCKET_PREFIX", "test-prefix")
	t.Setenv("OIBP_S3_UPLOAD_CONCURRENCY", "3")
	config := ApplicationConfig{}

	assert.Equal(t, "test-bucket", config.GetBucket())
	assert.Equal(t, "test-prefix", config.GetBucketPath())
	assert.Equal(t, 3, config.GetS3UploadConcurrency())
}

func TestApplicationConfigAdapterResponseSizeEnvironmentOverride(t *testing.T) {
	t.Setenv("OIBP_MAX_ADAPTER_RESPONSE_SIZE_BYTES", "1048576")
	config := ApplicationConfig{}

	assert.Equal(t, int64(1048576), config.GetMaximumAdapterResponseSizeBytes())
}

func TestApplicationConfigRejectsInvalidPositiveInteger(t *testing.T) {
	t.Setenv("OIBP_S3_UPLOAD_CONCURRENCY", "0")
	config := ApplicationConfig{}

	assert.PanicsWithValue(t, "OIBP_S3_UPLOAD_CONCURRENCY must be a positive integer", func() {
		config.GetS3UploadConcurrency()
	})
}

func TestApplicationConfigGetDocumentPath(t *testing.T) {
	config := ApplicationConfig{}

	actual := config.GetDocumentPath("test-bucket", "prefix/nested", "test-job", "pdf")

	assert.Equal(t, "s3://test-bucket/prefix/nested/jobs/test-job/document.pdf", actual)
}

func TestApplicationConfigGetOutputPath(t *testing.T) {
	config := ApplicationConfig{}

	actual := config.GetOutputPath("test-bucket", "prefix/nested", "test-job")

	assert.Equal(t, "s3://test-bucket/prefix/nested/jobs/test-job/result.json", actual)
}

func TestApplicationConfigGetQueueURL(t *testing.T) {
	t.Setenv("SQS_QUEUE_URL", "MY_URL")

	ac := ApplicationConfig{}
	url := ac.GetQueueURL()

	assert.Equal(t, "MY_URL", url)
}

func TestApplicationConfigGetQueueURLUnset(t *testing.T) {
	t.Setenv("SQS_QUEUE_URL", "")

	ac := ApplicationConfig{}
	assert.PanicsWithValue(t, "env var SQS_QUEUE_URL must be defined", func() {
		ac.GetQueueURL()
	})
}

func TestApplicationConfigGetSESConfigurationSetName(t *testing.T) {
	t.Setenv("SES_NOTIFICATION_CONFIGURATION_SET_NAME", "notifications")

	config := ApplicationConfig{}

	assert.Equal(t, "notifications", config.GetSESConfigurationSetName())
}

func TestApplicationConfigGetSESConfigurationSetNameUnset(t *testing.T) {
	t.Setenv("SES_NOTIFICATION_CONFIGURATION_SET_NAME", "")

	config := ApplicationConfig{}

	assert.PanicsWithValue(t, "env var SES_NOTIFICATION_CONFIGURATION_SET_NAME must be defined", func() {
		config.GetSESConfigurationSetName()
	})
}

func TestApplicationConfigGetSESAddressFrom(t *testing.T) {
	t.Setenv("SES_NOTIFICATION_ADDRESS_FROM", "notifications@example.com")

	config := ApplicationConfig{}

	assert.Equal(t, "notifications@example.com", config.GetSESAddressFrom())
}

func TestApplicationConfigGetSESAddressFromUnset(t *testing.T) {
	t.Setenv("SES_NOTIFICATION_ADDRESS_FROM", "")

	config := ApplicationConfig{}

	assert.PanicsWithValue(t, "env var SES_NOTIFICATION_ADDRESS_FROM must be defined", func() {
		config.GetSESAddressFrom()
	})
}

func TestApplicationConfigGetNotificationEmailLimitDefault(t *testing.T) {
	t.Setenv("OIBP_NOTIFICATION_EMAIL_LIMIT", "")

	config := ApplicationConfig{}

	assert.Equal(t, 5, config.GetNotificationEmailLimit())
}

func TestApplicationConfigGetNotificationEmailLimitEnvironmentOverride(t *testing.T) {
	t.Setenv("OIBP_NOTIFICATION_EMAIL_LIMIT", "10")

	config := ApplicationConfig{}

	assert.Equal(t, 10, config.GetNotificationEmailLimit())
}

func TestApplicationConfigGetNotificationEmailLimitRejectsInvalidValue(t *testing.T) {
	t.Setenv("OIBP_NOTIFICATION_EMAIL_LIMIT", "0")

	config := ApplicationConfig{}

	assert.PanicsWithValue(t, "OIBP_NOTIFICATION_EMAIL_LIMIT must be a positive integer", func() {
		config.GetNotificationEmailLimit()
	})
}

func TestApplicationConfigQueueDefaults(t *testing.T) {
	config := ApplicationConfig{}

	assert.Equal(t, int32(20), config.GetQueuePollingTime())
}

func TestApplicationConfigOtelDefaults(t *testing.T) {
	config := ApplicationConfig{}

	assert.Equal(t, "openitinerary-batch-api", config.GetOtelServiceName())
	assert.Equal(t, "openitinerary-batch-worker", config.GetOtelWorkerName())
	assert.Equal(t, "0.0.1", config.GetOtelServiceVersion())
	assert.Equal(t, 15*time.Second, config.GetOtelExportInterval())
}

func TestApplicationConfigOtelEnabledDefault(t *testing.T) {
	t.Setenv("OIBP_TELEMETRY_ENABLED", "")

	config := ApplicationConfig{}

	assert.True(t, config.GetOtelEnabled())
}

func TestApplicationConfigOtelEnabledEnvironmentOverride(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		{name: "enabled", value: "true", expected: true},
		{name: "disabled", value: "false", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("OIBP_TELEMETRY_ENABLED", tt.value)

			config := ApplicationConfig{}

			assert.Equal(t, tt.expected, config.GetOtelEnabled())
		})
	}
}

func TestApplicationConfigOtelEnabledInvalidValueUsesDefault(t *testing.T) {
	t.Setenv("OIBP_TELEMETRY_ENABLED", "not-a-bool")

	config := ApplicationConfig{}

	assert.True(t, config.GetOtelEnabled())
}

func TestGetLoggingLevelDefault(t *testing.T) {
	t.Setenv("OIBP_LOG_LEVEL", "")

	config := ApplicationConfig{}
	l := config.GetLoggingLevel()

	assert.Equal(t, slog.LevelInfo, l)
}

func TestGetLoggingLevelSet(t *testing.T) {
	t.Setenv("OIBP_LOG_LEVEL", "ERROR")

	config := ApplicationConfig{}
	l := config.GetLoggingLevel()

	assert.Equal(t, slog.LevelError, l)
}

func TestGetLoggingLevelSetInvalid(t *testing.T) {
	t.Setenv("OIBP_LOG_LEVEL", "HELLO")

	config := ApplicationConfig{}
	assert.Panics(t, func() {
		config.GetLoggingLevel()
	})
}

func TestGetS3PresignedExpirationSecondsDefault(t *testing.T) {
	t.Setenv("OIBP_S3_PRESIGNED_EXPIRE_SECONDS", "")

	config := ApplicationConfig{}
	value := config.GetS3PresignedExpirationSeconds()

	assert.Equal(t, 3600, value)
}

func TestGetS3PresignedExpirationSecondsSet(t *testing.T) {
	t.Setenv("OIBP_S3_PRESIGNED_EXPIRE_SECONDS", "60")

	config := ApplicationConfig{}
	value := config.GetS3PresignedExpirationSeconds()

	assert.Equal(t, 60, value)
}

func TestGetS3PresignedExpirationSecondsSetAboveMaximum(t *testing.T) {
	t.Setenv("OIBP_S3_PRESIGNED_EXPIRE_SECONDS", "604801")

	config := ApplicationConfig{}
	assert.Panics(t, func() {
		config.GetS3PresignedExpirationSeconds()
	})
}
