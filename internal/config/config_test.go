package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestApplicationConfigServerDefaults(t *testing.T) {
	config := ApplicationConfig{}

	assert.Equal(t, ":8080", config.GetPort())
	assert.Equal(t, 30*time.Second, config.GetServerReadTimeout())
	assert.Equal(t, 30*time.Second, config.GetServerWriteTimeout())
	assert.Equal(t, 15*time.Second, config.GetServerIdleTimeout())
}

func TestApplicationConfigUploadDefaults(t *testing.T) {
	config := ApplicationConfig{}

	assert.Equal(t, int64(10<<20), config.GetMaximumAllowedRequestSizeMb())
	assert.Equal(t, int64(5<<20), config.GetMaximumBufferMultipartForm())
}

func TestApplicationConfigStorageDefaults(t *testing.T) {
	t.Setenv("OIBP_BUCKET", "")
	t.Setenv("OIBP_BUCKET_PREFIX", "")
	config := ApplicationConfig{}

	assert.Equal(t, "openitinerary-batch-processing", config.GetBucket())
	assert.Empty(t, config.GetBucketPath())
}

func TestApplicationConfigStorageEnvironmentOverrides(t *testing.T) {
	t.Setenv("OIBP_BUCKET", "test-bucket")
	t.Setenv("OIBP_BUCKET_PREFIX", "test-prefix")
	config := ApplicationConfig{}

	assert.Equal(t, "test-bucket", config.GetBucket())
	assert.Equal(t, "test-prefix", config.GetBucketPath())
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
	assert.PanicsWithValue(t, "SQS_QUEUE_URL must be set", func() {
		ac.GetQueueURL()
	})
}

func TestApplicationConfigQueueDefaults(t *testing.T) {
	config := ApplicationConfig{}

	assert.Equal(t, int32(20), config.GetQueuePollingTime())
}
