package config

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEndpointStatus_LogValuer(t *testing.T) {
	endpoints := []EndpointV1{
		EndpointSubmit,
		EndpointStatus,
		EndpointResults,
	}

	for _, endpoint := range endpoints {
		t.Run(string(endpoint), func(t *testing.T) {
			v := slog.AnyValue(endpoint).Resolve()

			assert.Equal(t, slog.KindString, v.Kind())
			assert.Equal(t, string(endpoint), v.String())
		})
	}
}
