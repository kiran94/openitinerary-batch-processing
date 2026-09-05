package metrics

import "context"

type WorkerRunRecorder interface {
	/*
		Called when a Worker starts running
		contributes to metrics:
		* oibp.worker.runs
	*/
	RecordWorkerRun(ctx context.Context, o WorkerRunObservation)
}

type WorkerRunObservation struct {
	Outcome WorkerRunOutcome
}

type WorkerRunOutcome string

const (
	WorkerRunOutcomeSuccess WorkerRunOutcome = "success"
	WorkerRunOutcomeFailed  WorkerRunOutcome = "failed"
)
