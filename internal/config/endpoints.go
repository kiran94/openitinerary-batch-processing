package config

import "log/slog"

type EndpointV1 string

const (
	EndpointSubmit  EndpointV1 = "/api/v1/submit"
	EndpointStatus  EndpointV1 = "/api/v1/status/{id}"
	EndpointResults EndpointV1 = "/api/v1/result/{id}"
)

func (e EndpointV1) LogValue() slog.Value {
	return slog.StringValue(string(e))
}
