package metrics

import (
	"context"

	"github.com/kiran94/openitinerary-batch-processing/internal/config"
)

type ResponseObservation struct {
	Endpoint config.EndpointV1
}

type ResponseWriteErrorRecorder interface {
	/*
		Called when the endpoint fails when writing the response
		to the caller.
		contributes to:
		* oibp.api.response_write.errors
	*/
	RecordResponseWriteError(context.Context, ResponseObservation)
}
