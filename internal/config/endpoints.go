package config

type EndpointV1 string

const (
	EndpointSubmit  EndpointV1 = "/api/v1/submit"
	EndpointStatus  EndpointV1 = "/api/v1/status/{id}"
	EndpointResults EndpointV1 = "/api/v1/result/{id}"
)
