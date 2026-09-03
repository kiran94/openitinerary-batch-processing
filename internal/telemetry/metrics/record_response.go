package metrics

import (
	"context"
)

type ResponseObservation struct {
	Endpoint EndpointV1
}

type ResponseWriteErrorRecorder interface {
	/*
		Called when the endpoint fails when writing the response
		to the caller.
		contributes to:
		* oibp.api.response_write.errors
	*/
	RecordResponseWriteErrorObservation(context.Context, ResponseObservation)
}

type EndpointV1 string

const (
	EndpointSubmit  EndpointV1 = "/api/v1/submit"
	EndpointStatus  EndpointV1 = "/api/v1/status/{id}"
	EndpointResults EndpointV1 = "/api/v1/result/{id}"
)
