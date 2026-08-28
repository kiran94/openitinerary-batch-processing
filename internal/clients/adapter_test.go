package clients

import (
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
var errRequest = errors.New("request failed")
var errResponseClose = errors.New("response close failed")

type failingDocumentReader struct{}

func (failingDocumentReader) Read([]byte) (int, error) {
	return 0, errDocumentRead
}

type trackingReadCloser struct {
	io.Reader
	closed   bool
	closeErr error
}

func (r *trackingReadCloser) Close() error {
	r.closed = true
	return r.closeErr
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func newAdapterResponseClient(statusCode int, responseBody io.ReadCloser) Adapter {
	return &AdapterClient{
		adapter_api_url: "http://adapter.test",
		http: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				defer request.Body.Close()
				_, _ = io.Copy(io.Discard, request.Body)

				return &http.Response{
					StatusCode: statusCode,
					Body:       responseBody,
					Header:     make(http.Header),
					Request:    request,
				}, nil
			}),
		},
	}
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

	response, err := adapter.Convert(context.Background(), nil, "filename", "contentType")

	require.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "document was nil")
}

func TestConvertStatusCodeNotOk(t *testing.T) {
	responseBody := &trackingReadCloser{
		Reader:   strings.NewReader("unprocessable"),
		closeErr: errResponseClose,
	}
	adapter := newAdapterResponseClient(http.StatusUnprocessableEntity, responseBody)

	mockDocument := strings.NewReader("mock")
	response, err := adapter.Convert(context.Background(), mockDocument, "filename", "contentType")

	require.Error(t, err)
	require.ErrorIs(t, err, errResponseClose)
	assert.Nil(t, response)
	assert.True(t, responseBody.closed)
}

func TestConvertRequestErrorWithNilResponse(t *testing.T) {
	adapter := &AdapterClient{
		adapter_api_url: "http://adapter.test",
		http: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				_ = request.Body.Close()
				return nil, errRequest
			}),
		},
	}

	response, err := adapter.Convert(
		context.Background(),
		strings.NewReader("PDF contents"),
		"example.pdf",
		"application/pdf",
	)

	require.ErrorIs(t, err, errRequest)
	assert.Nil(t, response)
}

func TestConvertStatusCodeOk(t *testing.T) {

	adapter := newAdapterTestClient(t, func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, int64(-1), r.ContentLength)

		file, header, err := r.FormFile("file")
		if !assert.NoError(t, err) {
			return
		}
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
		assert.NoError(t, err)
	})

	response, err := adapter.Convert(
		context.Background(),
		strings.NewReader("PDF contents"),
		"example.pdf",
		"application/pdf",
	)

	require.NoError(t, err)
	require.NotNil(t, response)
	defer response.Close()

	result, err := io.ReadAll(response)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"type":"flight"}]`, string(result))
}

func TestConvertDocumentReadError(t *testing.T) {
	responseBody := &trackingReadCloser{Reader: strings.NewReader("unused")}
	adapter := newAdapterResponseClient(http.StatusOK, responseBody)

	response, err := adapter.Convert(
		context.Background(),
		failingDocumentReader{},
		"example.pdf",
		"application/pdf",
	)

	require.ErrorIs(t, err, errDocumentRead)
	assert.Nil(t, response)
	assert.True(t, responseBody.closed)
}
