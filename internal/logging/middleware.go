package logging

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const HeaderRequestID = "X-Request-ID"

func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(HeaderRequestID)
		if requestID == "" {
			id, err := uuid.NewRandom()
			if err != nil {
				http.Error(w, "failed to create request id", http.StatusInternalServerError)
				return
			}

			requestID = id.String()
			r.Header.Set(HeaderRequestID, requestID)
		} else {
			_, err := uuid.Parse(requestID)
			if err != nil {
				http.Error(w, fmt.Sprintf("supplied request id was not a valid uuid. Got: %v", requestID), http.StatusBadRequest)
				return
			}
		}

		ctx := WithRequestID(r.Context(), requestID)
		r = r.WithContext(ctx)

		w.Header().Set(HeaderRequestID, requestID)
		next.ServeHTTP(w, r)
	})
}

// RequestIDSpanMiddleware looks to see if there is a request id and if there is then attaches it to the span
// This functionality cannot go inside RequestIDMiddleware because
// Request-ID middleware must run first because placing it inside `otelhttp` creates a copied request,
// preventing `otelhttp` from seeing the route pattern later set by `ServeMux`.
func RequestIDSpanMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if requestID, ok := GetRequestID(ctx); ok {
			span := trace.SpanFromContext(ctx)
			span.SetAttributes(attribute.String("request_id", requestID))
		}

		next.ServeHTTP(w, r)
	})
}
