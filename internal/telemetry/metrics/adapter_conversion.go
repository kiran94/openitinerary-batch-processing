package metrics

import (
	"context"
	"time"
)

// AdapterConversionObservation describes one call to the adapter's Convert
// operation. It measures the adapter call itself, not consumption of the
// response returned by the adapter.
type AdapterConversionObservation struct {
	Duration time.Duration
	Outcome  AdapterConversionOutcome
}

type AdapterConversionRecorder interface {
	/*
		Called after an adapter conversion attempt
		contributes to metrics:
		* oibp.adapter.convert.duration
	*/
	RecordAdapterConversion(context.Context, AdapterConversionObservation)
}

type AdapterConversionOutcome string

const (
	AdapterConversionOutcomeSuccess AdapterConversionOutcome = "success"
	AdapterConversionOutcomeFailed  AdapterConversionOutcome = "failed"
)
