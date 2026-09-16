package metrics

import (
	"context"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
)

type AdapterResponseRecorder interface {
	/*
		Called after an adapter response has been fully consumed
		contributes to metrics:
		* oibp.adapter.response.size
	*/
	RecordAdapterResponse(context.Context, AdapterResponseObservation)
}

// AdapterResponseObservation describes the complete response body produced by
// the adapter. SizeBytes is recorded only after the response has been fully
// consumed successfully.
type AdapterResponseObservation struct {
	SizeBytes   int64
	ContentType config.ContentType
}
