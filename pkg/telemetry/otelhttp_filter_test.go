package telemetry

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterRequests(t *testing.T) {
	tests := []struct {
		path        string
		shouldTrace bool
	}{
		{path: "/health/live", shouldTrace: false},
		{path: "/health/ready", shouldTrace: false},
		{path: "/api/v1/submit", shouldTrace: true},
		{path: "/api/v1/status", shouldTrace: true},
		{path: "/api/v1/result", shouldTrace: true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, http.NoBody)

			ok := FilterRequests(req)
			assert.Equal(t, tt.shouldTrace, ok)
		})
	}
}
