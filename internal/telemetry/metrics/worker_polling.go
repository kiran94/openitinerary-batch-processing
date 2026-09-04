package metrics

import (
	"context"
	"time"
)

type WorkerPollingRecorder interface {
	/*
		Called after each queue polling attempt
		contributes to metrics:
		* oibp.worker.polling.duration
	*/
	RecordWorkerPolling(ctx context.Context, o WorkerPollingObservation)
}

type WorkerPollingObservation struct {
	Duration time.Duration
	Outcome  WorkerPollingOutcome
}

type WorkerPollingOutcome string

const (
	WorkerPollingOutcomeSuccess   WorkerPollingOutcome = "success"
	WorkerPollingOutcomeFailed    WorkerPollingOutcome = "failed"
	WorkerPollingOutcomeCancelled WorkerPollingOutcome = "cancelled"
)
