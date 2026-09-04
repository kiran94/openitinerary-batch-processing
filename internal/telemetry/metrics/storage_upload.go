package metrics

import (
	"context"
	"time"
)

type StorageUploadObservation struct {
	Duration time.Duration
	Purpose  StorageUploadPurpose
	Outcome  StorageUploadOutcome
}

type StorageUploadRecorder interface {
	/*
		Called when an upload is performed on storage
		contributes to metrics:
		* oibp.storage.upload.duration
	*/
	RecordStorageUpload(context.Context, StorageUploadObservation)
}

type StorageUploadPurpose string

const (
	StorageUploadPurposeDocument StorageUploadPurpose = "document"
	StorageUploadPurposeResult   StorageUploadPurpose = "result"
)

type StorageUploadOutcome string

const (
	StorageUploadSuccess StorageUploadOutcome = "success"
	StorageUploadFailed  StorageUploadOutcome = "failed"
)
