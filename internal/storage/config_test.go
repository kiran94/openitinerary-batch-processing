package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetDocumentPath(t *testing.T) {
	sc := StorageConfiguration{
		"my_bucket",
		"prefix/prefix2",
	}

	output := sc.GetDocumentPath("jobId")
	assert.Equal(t, "s3://my_bucket/prefix/prefix2/documents/jobId", output)
}

func TestGetOutputPath(t *testing.T) {
	sc := StorageConfiguration{
		"my_bucket",
		"prefix/prefix2",
	}

	output := sc.GetOutputPath("jobId")
	assert.Equal(t, "s3://my_bucket/prefix/prefix2/outputs/jobId", output)
}
