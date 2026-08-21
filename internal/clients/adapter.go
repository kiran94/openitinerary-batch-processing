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
)

type Adapter interface {
	Convert(ctx context.Context, document io.Reader, filename string, contentType string, w io.WriteCloser) error
}

type AdapterClient struct {
	adapter_api_url string
	http            *http.Client
}

func NewAdapterClient(http *http.Client) Adapter {
	return &AdapterClient{
		adapter_api_url: getAdapterApiUrl(),
		http:            http,
	}
}

// Convert uploads a document as a multipart form and returns the response body.
// It uses an io.Pipe so one goroutine can write the multipart upload while the
// HTTP client reads and sends it. This avoids loading the whole document into
// memory. Closing the pipe writer tells the HTTP client that the upload is
// complete, or passes along an error if reading the document fails.
//
// document reader → multipart writer → io.Pipe → HTTP client → Adapter API
func (a *AdapterClient) Convert(ctx context.Context, document io.Reader, filename string, contentType string, w io.WriteCloser) (err error) {
	defer func() {
		err = errors.Join(err, w.Close())
	}()

	if document == nil {
		return errors.New("document was nil")
	}

	readerBuffer, writerBuffer := io.Pipe()
	writer := multipart.NewWriter(writerBuffer)

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, a.adapter_api_url, readerBuffer)
	if err != nil {
		_ = readerBuffer.Close()
		_ = writerBuffer.Close()
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())

	// Start to write the document to the writer in a Go routine.
	uploadDone := make(chan error, 1)
	go func() {
		uploadErr := writeMultipart(writer, document, filename, contentType)

		writerBuffer.CloseWithError(uploadErr)
		uploadDone <- uploadErr
	}()

	response, requestErr := a.http.Do(request)

	// Ensure the producer is released if the server or transport stops
	// consuming the request body before the upload completes.
	_ = readerBuffer.Close()
	uploadErr := <-uploadDone

	if requestErr != nil {
		if response != nil {
			_ = response.Body.Close()
		}

		if uploadErr != nil && !errors.Is(uploadErr, io.ErrClosedPipe) {
			return uploadErr
		}

		return requestErr
	}
	defer func() {
		err = errors.Join(err, response.Body.Close())
	}()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("response from adapter API was not okay. Got %v", response.Status)
	}

	if uploadErr != nil {
		return uploadErr
	}

	if _, err := io.Copy(w, response.Body); err != nil {
		return err
	}

	return nil
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

func getAdapterApiUrl() string {
	const ADAPTER_API = "http://127.0.0.1:8000/api/v1/adapt/segment"

	value := os.Getenv("ADAPTER_API_URL")
	if value == "" {
		return ADAPTER_API
	}

	return value
}
