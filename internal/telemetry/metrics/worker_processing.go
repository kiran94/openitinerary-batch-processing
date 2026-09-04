package metrics

import (
	"context"
	"time"
)

type WorkerProcessingRecorder interface {
	/*
		Called after processing a valid, non-complete job attempt
		contributes to metrics:
		* oibp.worker.processing.duration
	*/
	RecordWorkerProcessing(context.Context, WorkerProcessingObservation)
}

type WorkerProcessingObservation struct {
	Duration time.Duration
	Outcome  WorkerProcessingOutcome
}

type WorkerProcessingOutcome string

const (
	WorkerProcessingOutcomeSuccess WorkerProcessingOutcome = "success"
	WorkerProcessingOutcomeFailed  WorkerProcessingOutcome = "failed"
)
