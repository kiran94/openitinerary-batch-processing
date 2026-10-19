package telemetry

import (
	"net/http"
)

var excludedPaths = []string{"/health/live", "/health/ready"}

func FilterRequests(r *http.Request) bool {
	for _, path := range excludedPaths {
		if r.URL.Path == path {
			return false
		}
	}

	return true
}
