package logging

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestIDMiddlewareRequestIDNotSet(t *testing.T) {
	handlerCalled := false
	dummyPayload := []byte("success")
	handler := RequestIDMiddleware(dummyRoute(t, "", dummyPayload, &handlerCalled))

	server := httptest.NewServer(handler)
	client := server.Client()

	response, err := client.Get(server.URL)
	require.NoError(t, err)

	defer response.Body.Close()

	result, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, dummyPayload, result)

	_, err = uuid.Parse(response.Header.Get(HeaderRequestID))
	require.NoError(t, err)
	assert.True(t, handlerCalled)
}

func TestRequestIDMiddlewareRequestIDSet(t *testing.T) {
	const requestID = "5b2f4220-5db9-4f66-b781-ebac11a5afea"

	handlerCalled := false
	dummyPayload := []byte("success")
	handler := RequestIDMiddleware(dummyRoute(t, requestID, []byte("success"), &handlerCalled))

	server := httptest.NewServer(handler)
	client := server.Client()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, http.NoBody)
	require.NoError(t, err)
	request.Header.Set(HeaderRequestID, requestID)

	response, err := client.Do(request)
	require.NoError(t, err)

	defer response.Body.Close()

	result, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.Equal(t, dummyPayload, result)

	id, err := uuid.Parse(response.Header.Get(HeaderRequestID))
	require.NoError(t, err)

	assert.Equal(t, requestID, id.String())
	assert.True(t, handlerCalled)
}

func TestRequestIDMiddlewareInvalidInputRequestID(t *testing.T) {
	const requestID = "not_a_request_id"

	handlerCalledFlag := false
	handler := RequestIDMiddleware(dummyRoute(t, requestID, []byte("dummy"), &handlerCalledFlag))
	server := httptest.NewServer(handler)
	client := server.Client()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL, http.NoBody)
	require.NoError(t, err)
	request.Header.Set(HeaderRequestID, requestID)

	response, err := client.Do(request)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)

	b, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	assert.Contains(t, string(b), "supplied request id was not a valid")
	assert.False(t, handlerCalledFlag)
}

func dummyRoute(t *testing.T, requestID string, payload []byte, calledFlag *bool) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		*calledFlag = true

		// Check the generated requestid was set to context
		i, ok := GetRequestID(r.Context())
		assert.True(t, ok)

		if requestID != "" {
			assert.Equal(t, requestID, i)
		}

		_, err := w.Write(payload)
		if err != nil {
			http.Error(w, "failed to write dummy payload", http.StatusInternalServerError)
		}

	}
}
