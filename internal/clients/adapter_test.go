package clients

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAdapterTestClient(t *testing.T, handler http.HandlerFunc) Adapter {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &AdapterClient{
		adapter_api_url: server.URL,
		http:            server.Client(),
	}
}

func TestNewAdapterClient(t *testing.T) {
	http := &http.Client{}
	adapter := NewAdapterClient(http)

	client, ok := adapter.(*AdapterClient)

	assert.True(t, ok)
	assert.NotNil(t, client)
}

func TestConvertNilDocument(t *testing.T) {
	adapter := newAdapterTestClient(t, func(w http.ResponseWriter, r *http.Request) {})

	_, err := adapter.Convert(nil, "filename", "contentType")
	assert.Error(t, err, "document was nil")
}

func TestConvertStatusCodeNotOk(t *testing.T) {

	adapter := newAdapterTestClient(t, func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	})

	mockDocument := strings.NewReader("mock")
	_, err := adapter.Convert(mockDocument, "filename", "contentType")
	assert.Error(t, err)
}

func TestConvertStatusCodeOk(t *testing.T) {

	adapter := newAdapterTestClient(t, func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		assert.Equal(t, http.MethodPost, r.Method)

		file, header, err := r.FormFile("file")
		require.NoError(t, err)
		defer file.Close()

		assert.Equal(t, "example.pdf", header.Filename)
		assert.Equal(
			t,
			"application/pdf",
			header.Header.Get("Content-Type"),
		)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		_, err = w.Write([]byte(`[{"type":"flight"}]`))
		require.NoError(t, err)
	})

	result, err := adapter.Convert(
		strings.NewReader("PDF contents"),
		"example.pdf",
		"application/pdf",
	)

	require.NoError(t, err)

	assert.JSONEq(t, `[{"type":"flight"}]`, string(*result))
}
