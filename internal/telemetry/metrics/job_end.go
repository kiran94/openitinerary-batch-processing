package metrics

import (
	"context"
	"time"
)

type JobEndToEndRecorder interface {
	/*
		Called when a job reaches a terminal outcome
		contributes to metrics:
		* oibp.job.end_to_end.duration
	*/
	RecordJobEndToEnd(ctx context.Context, o JobEndObservation)
}

type JobEndObservation struct {
	Duration time.Duration
	Outcome  JobEndOutcome
	Reason   *TerminalReason
}

type JobEndOutcome string

const (
	JobEndOutcomeSuccess JobEndOutcome = "success"
	JobEndOutcomeFailed  JobEndOutcome = "failed"
)
