package metrics

import "context"

type TerminalObservation struct {
	Outcome TerminalOutcome
	Reason  *TerminalReason
}

type JobTerminalRecorder interface {
	/*
		Called when processing stops
		contributes to metrics:
		* oibp.jobs.terminal

	*/
	RecordJobTerminal(context.Context, TerminalObservation)
}

type TerminalOutcome string

const (
	TerminalSuccess TerminalOutcome = "success"
	TerminalFailed  TerminalOutcome = "failed"
)

type TerminalReason string

const (
	TerminalReasonEnqueueFailed      TerminalReason = "enqueue_failed"
	TerminalReasonStorageReadFailed  TerminalReason = "storage_read_failed"
	TerminalReasonStorageReadNil     TerminalReason = "storage_read_nil"
	TerminalReasonConvertFailed      TerminalReason = "convert_failed"
	TerminalReasonResponseReaderNil  TerminalReason = "response_reader_nil"
	TerminalReasonStorageWriteFailed TerminalReason = "storage_write_failed"
)
