package logging

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"
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
