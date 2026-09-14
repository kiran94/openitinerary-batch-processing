package telemetry

import (
	"net/http"
)

func FilterRequests(r *http.Request) bool {
	excludedPaths := []string{
		"/health/live",
		"/health/ready",
	}

	for _, path := range excludedPaths {
		if r.URL.Path == path {
			return false
		}
	}

	return true
}
