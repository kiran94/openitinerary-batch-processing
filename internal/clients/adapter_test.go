package clients

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errDocumentRead = errors.New("document read failed")
var errRequest = errors.New("request failed")
var errResponseClose = errors.New("response close failed")

const maximumAdapterResponseSizeBytes = int64(1 << 20)

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

func newAdapterResponseClient(t *testing.T, statusCode int, contentLength int64, responseBody io.ReadCloser) Adapter {
	t.Helper()

	return &AdapterClient{
		adapterAPIURL:                   "http://adapter.test",
		logger:                          &mockLogger{},
		maximumAdapterResponseSizeBytes: maximumAdapterResponseSizeBytes,
		http: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				defer request.Body.Close()
				_, _ = io.Copy(io.Discard, request.Body)

				requestID := request.Header.Get(logging.HeaderRequestID)
				assert.NotEmpty(t, requestID)

				return &http.Response{
					StatusCode:    statusCode,
					ContentLength: contentLength,
					Body:          responseBody,
					Header:        make(http.Header),
					Request:       request,
				}, nil
			}),
		},
		tracer: testutils.NewMockTracer(),
	}
}

func newAdapterTestClient(t *testing.T, handler http.HandlerFunc) Adapter {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &AdapterClient{
		adapterAPIURL:                   server.URL,
		logger:                          &mockLogger{},
		maximumAdapterResponseSizeBytes: maximumAdapterResponseSizeBytes,
		http:                            server.Client(),
		tracer:                          testutils.NewMockTracer(),
	}
}

func TestNewAdapterClient(t *testing.T) {
	http := &http.Client{}
	mockTracer := testutils.NewMockTracer()
	adapter := NewAdapterClient(http, &mockLogger{}, mockTracer, maximumAdapterResponseSizeBytes)

	client, ok := adapter.(*AdapterClient)

	assert.True(t, ok)
	assert.NotNil(t, client)
}

func TestConvertNilDocument(t *testing.T) {
	adapter := newAdapterTestClient(t, func(w http.ResponseWriter, r *http.Request) {})

	ctx := populateContext(t, context.Background())
	response, err := adapter.Convert(ctx, nil, "filename", "contentType")

	require.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "document was nil")
}

func TestConvertStatusCodeNotOk(t *testing.T) {
	responseBody := &trackingReadCloser{
		Reader:   strings.NewReader("unprocessable"),
		closeErr: errResponseClose,
	}
	adapter := newAdapterResponseClient(t, http.StatusUnprocessableEntity, -1, responseBody)

	mockDocument := strings.NewReader("mock")

	ctx := populateContext(t, context.Background())
	response, err := adapter.Convert(ctx, mockDocument, "filename", "contentType")

	require.Error(t, err)
	require.ErrorIs(t, err, errResponseClose)
	assert.Nil(t, response)
	assert.True(t, responseBody.closed)
}

func TestConvertRequestErrorWithNilResponse(t *testing.T) {
	adapter := &AdapterClient{
		adapterAPIURL:                   "http://adapter.test",
		logger:                          &mockLogger{},
		maximumAdapterResponseSizeBytes: maximumAdapterResponseSizeBytes,
		http: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				_ = request.Body.Close()
				return nil, errRequest
			}),
		},
		tracer: testutils.NewMockTracer(),
	}

	ctx := populateContext(t, context.Background())
	response, err := adapter.Convert(
		ctx,
		strings.NewReader("PDF contents"),
		"example.pdf",
		"application/pdf",
	)

	require.ErrorIs(t, err, errRequest)
	assert.Nil(t, response)
}

func TestConvertStatusCodeOk(t *testing.T) {
	requestID := uuid.NewString()

	adapter := newAdapterTestClient(t, func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, requestID, r.Header.Get(logging.HeaderRequestID))
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

	ctx := logging.WithRequestID(context.Background(), requestID)
	response, err := adapter.Convert(
		ctx,
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

func TestConvertOmitsRequestIDWithoutContextValue(t *testing.T) {
	var requestID string
	adapter := &AdapterClient{
		adapterAPIURL:                   "http://adapter.test",
		logger:                          &mockLogger{},
		maximumAdapterResponseSizeBytes: maximumAdapterResponseSizeBytes,
		http: &http.Client{
			Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requestID = request.Header.Get(logging.HeaderRequestID)
				defer request.Body.Close()
				_, err := io.Copy(io.Discard, request.Body)
				if err != nil {
					return nil, err
				}

				return &http.Response{
					StatusCode:    http.StatusOK,
					ContentLength: -1,
					Body:          io.NopCloser(strings.NewReader("[]")),
					Header:        make(http.Header),
					Request:       request,
				}, nil
			}),
		},
		tracer: testutils.NewMockTracer(),
	}

	response, err := adapter.Convert(
		context.Background(),
		strings.NewReader("document"),
		"example.pdf",
		"application/pdf",
	)

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Empty(t, requestID)
	require.NoError(t, response.Close())
}

func TestConvertRejectsKnownOversizedResponse(t *testing.T) {
	responseBody := &trackingReadCloser{Reader: strings.NewReader("123456")}
	adapter := newAdapterResponseClient(t, http.StatusOK, 6, responseBody).(*AdapterClient)
	adapter.maximumAdapterResponseSizeBytes = 5

	ctx := populateContext(t, context.Background())

	response, err := adapter.Convert(
		ctx,
		strings.NewReader("document"),
		"example.pdf",
		"application/pdf",
	)

	var maxBytesErr *http.MaxBytesError
	require.ErrorAs(t, err, &maxBytesErr)
	assert.Equal(t, int64(5), maxBytesErr.Limit)
	assert.Nil(t, response)
	assert.True(t, responseBody.closed)
}

func TestConvertRejectsStreamedOversizedResponse(t *testing.T) {
	responseBody := &trackingReadCloser{Reader: strings.NewReader("123456")}
	adapter := newAdapterResponseClient(t, http.StatusOK, -1, responseBody).(*AdapterClient)
	adapter.maximumAdapterResponseSizeBytes = 5

	ctx := populateContext(t, context.Background())

	response, err := adapter.Convert(
		ctx,
		strings.NewReader("document"),
		"example.pdf",
		"application/pdf",
	)
	require.NoError(t, err)

	result, readErr := io.ReadAll(response)
	var maxBytesErr *http.MaxBytesError
	require.ErrorAs(t, readErr, &maxBytesErr)
	assert.Equal(t, "12345", string(result))
	assert.Equal(t, int64(5), maxBytesErr.Limit)
	require.NoError(t, response.Close())
	assert.True(t, responseBody.closed)
}

func TestConvertAcceptsResponseAtMaximumSize(t *testing.T) {
	responseBody := &trackingReadCloser{Reader: strings.NewReader("12345")}
	adapter := newAdapterResponseClient(t, http.StatusOK, -1, responseBody).(*AdapterClient)
	adapter.maximumAdapterResponseSizeBytes = 5

	ctx := populateContext(t, context.Background())

	response, err := adapter.Convert(
		ctx,
		strings.NewReader("document"),
		"example.pdf",
		"application/pdf",
	)
	require.NoError(t, err)

	result, err := io.ReadAll(response)
	require.NoError(t, err)
	assert.Equal(t, "12345", string(result))
	require.NoError(t, response.Close())
}

func TestConvertDocumentReadError(t *testing.T) {
	responseBody := &trackingReadCloser{Reader: strings.NewReader("unused")}
	adapter := newAdapterResponseClient(t, http.StatusOK, -1, responseBody)

	ctx := populateContext(t, context.Background())

	response, err := adapter.Convert(
		ctx,
		failingDocumentReader{},
		"example.pdf",
		"application/pdf",
	)

	require.ErrorIs(t, err, errDocumentRead)
	assert.Nil(t, response)
	assert.True(t, responseBody.closed)
}

func populateContext(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	return logging.WithRequestID(ctx, uuid.NewString())
}

type mockLogger struct{}

func (*mockLogger) Debug(string, ...any)                         {}
func (*mockLogger) Info(string, ...any)                          {}
func (*mockLogger) Warn(string, ...any)                          {}
func (*mockLogger) Error(string, ...any)                         {}
func (m *mockLogger) WithContext(context.Context) logging.Logger { return m }
