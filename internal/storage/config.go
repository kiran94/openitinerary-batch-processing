package storage

import (
	"os"
	"path/filepath"
)

func GetAWSS3Bucket() string {
	return GetEnvOrDefault("OIBP_BUCKET", "thebucketofkiran456")
}

func GetAWSS3Path() string {
	return GetEnvOrDefault("OIBP_BUCKET_PREFIX", "openitinerary")
}

func GetEnvOrDefault(envVarName, defaultValue string) string {
	value := os.Getenv(envVarName)
	if value == "" {
		value = defaultValue
	}

	return value
}

type StorageConfiguration struct {
	bucket     string
	bucketPath string
}

func NewStorageConfiguration() StorageConfiguration {
	return StorageConfiguration{
		bucket:     GetAWSS3Bucket(),
		bucketPath: GetAWSS3Path(),
	}
}

// s3://<bucket>/<prefix>/jobs/<job-id>/document.<format>
// s3://<bucket>/<prefix>/jobs/<job-id>/result.json

func (sc *StorageConfiguration) GetDocumentPath(jobId, fileType string) string {
	return "s3://" + filepath.Join(sc.bucket, sc.bucketPath, "jobs", jobId, "document"+"."+fileType)
}

func (sc *StorageConfiguration) GetOutputPath(jobId string) string {
	return "s3://" + filepath.Join(sc.bucket, sc.bucketPath, "jobs", jobId, "result.json")
}
