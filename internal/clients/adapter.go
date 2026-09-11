package clients

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"

	"github.com/kiran94/openitinerary-batch-processing/internal/logging"
	"github.com/kiran94/openitinerary-batch-processing/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Adapter interface {
	Convert(ctx context.Context, document io.Reader, filename string, contentType string) (io.ReadCloser, error)
}

type AdapterClient struct {
	adapterAPIURL                   string
	maximumAdapterResponseSizeBytes int64
	http                            *http.Client
	logger                          logging.Logger
	tracer                          trace.Tracer
}

func NewAdapterClient(http *http.Client, logger logging.Logger, tracer trace.Tracer, maximumAdapterResponseSizeBytes int64) Adapter {
	return &AdapterClient{
		adapterAPIURL:                   getAdapterAPIURL(),
		maximumAdapterResponseSizeBytes: maximumAdapterResponseSizeBytes,
		http:                            http,
		logger:                          logger,
		tracer:                          tracer,
	}
}

// Convert uploads a document as a multipart form and returns the response body.
// It uses an io.Pipe so one goroutine can write the multipart upload while the
// HTTP client reads and sends it. This avoids loading the whole document into
// memory. Closing the pipe writer tells the HTTP client that the upload is
// complete, or passes along an error if reading the document fails.
//
// document reader → multipart writer → io.Pipe → HTTP client → Adapter API
func (a *AdapterClient) Convert(ctx context.Context, document io.Reader, filename string, contentType string) (w io.ReadCloser, err error) {
	ctx, span := a.tracer.Start(
		ctx,
		string(telemetry.TracingAdaptDocument),
		trace.WithAttributes(attribute.String("content_type", contentType)),
		trace.WithSpanKind(trace.SpanKindClient),
	)
	defer span.End()

	logger := a.logger.WithContext(ctx)

	if document == nil {
		return nil, errors.New("document was nil")
	}

	readerBuffer, writerBuffer := io.Pipe()
	writer := multipart.NewWriter(writerBuffer)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.adapterAPIURL, readerBuffer)
	if err != nil {
		_ = readerBuffer.Close()
		_ = writerBuffer.Close()
		return nil, err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if v, ok := logging.GetRequestID(ctx); ok {
		request.Header.Set(logging.HeaderRequestID, v)
	}

	// Start to write the document to the writer in a Go routine.
	uploadDone := make(chan error, 1)
	go func() {
		uploadErr := writeMultipart(writer, document, filename, contentType)

		writerBuffer.CloseWithError(uploadErr)
		uploadDone <- uploadErr
	}()

	logger.Debug(
		"calling adapter api",
		"component", logging.ComponentAdapter,
		"operation", logging.OperationAdapterConvert,
		"endpoint", a.adapterAPIURL,
	)
	response, requestErr := a.http.Do(request)

	// Ensure the producer is released if the server or transport stops
	// consuming the request body before the upload completes.
	_ = readerBuffer.Close()
	uploadErr := <-uploadDone

	if requestErr != nil {
		if uploadErr != nil && !errors.Is(uploadErr, io.ErrClosedPipe) {
			return nil, closeResponseBody(response, uploadErr)
		}

		return nil, closeResponseBody(response, requestErr)
	}

	if response.StatusCode != http.StatusOK {
		err := fmt.Errorf("response from adapter API was not okay. Got %v", response.Status)
		return nil, closeResponseBody(response, err)
	}

	if uploadErr != nil {
		return nil, closeResponseBody(response, uploadErr)
	}
	if response.ContentLength > a.maximumAdapterResponseSizeBytes {
		err := &http.MaxBytesError{Limit: a.maximumAdapterResponseSizeBytes}
		return nil, closeResponseBody(response, err)
	}

	w = http.MaxBytesReader(nil, response.Body, a.maximumAdapterResponseSizeBytes)
	return w, nil
}

func closeResponseBody(response *http.Response, primaryErr error) error {
	if response == nil || response.Body == nil {
		return primaryErr
	}

	return errors.Join(primaryErr, response.Body.Close())
}

func writeMultipart(writer *multipart.Writer, document io.Reader, filename string, contentType string) error {
	headers := make(textproto.MIMEHeader)
	headers.Set("Content-Disposition", mime.FormatMediaType(
		"form-data",
		map[string]string{
			"name":     "file",
			"filename": filepath.Base(filename),
		},
	))
	headers.Set("Content-Type", contentType)

	part, err := writer.CreatePart(headers)
	if err != nil {
		return err
	}

	if _, err := io.Copy(part, document); err != nil {
		return err
	}

	// Closes the Multipart document, ready for upload
	return writer.Close()
}

func getAdapterAPIURL() string {
	const AdapterAPI = "http://127.0.0.1:8000/api/v1/adapt/segment"

	value := os.Getenv("ADAPTER_API_URL")
	if value == "" {
		return AdapterAPI
	}

	return value
}
