package clients

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errDocumentRead = errors.New("document read failed")

type failingDocumentReader struct{}

func (failingDocumentReader) Read([]byte) (int, error) {
	return 0, errDocumentRead
}

type trackingWriteCloser struct {
	bytes.Buffer
	closed bool
}

func (w *trackingWriteCloser) Close() error {
	w.closed = true
	return nil
}

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

	err := adapter.Convert(context.Background(), nil, "filename", "contentType", &trackingWriteCloser{})
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
	err := adapter.Convert(context.Background(), mockDocument, "filename", "contentType", &trackingWriteCloser{})
	assert.Error(t, err)
}

func TestConvertStatusCodeOk(t *testing.T) {

	adapter := newAdapterTestClient(t, func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, int64(-1), r.ContentLength)

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

	result := &trackingWriteCloser{}
	err := adapter.Convert(
		context.Background(),
		strings.NewReader("PDF contents"),
		"example.pdf",
		"application/pdf",
		result,
	)

	require.NoError(t, err)

	assert.JSONEq(t, `[{"type":"flight"}]`, result.String())
	assert.True(t, result.closed)
}

func TestConvertDocumentReadError(t *testing.T) {
	adapter := newAdapterTestClient(t, func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	})

	result := &trackingWriteCloser{}
	err := adapter.Convert(
		context.Background(),
		failingDocumentReader{},
		"example.pdf",
		"application/pdf",
		result,
	)

	require.ErrorIs(t, err, errDocumentRead)
	assert.True(t, result.closed)
}
