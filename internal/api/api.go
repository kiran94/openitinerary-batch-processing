package api

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"time"
)

type Adapter interface {
	Convert(document io.Reader, filename string, contentType string) (*[]byte, error)
}

type AdapterClient struct {
	adapter_api_url string
}

func NewAdapterClient() *AdapterClient {
	return &AdapterClient{
		getAdapterApiUrl(),
	}
}

func (a *AdapterClient) Convert(document io.Reader, filename string, contentType string) (*[]byte, error) {
	if document == nil {
		return nil, errors.New("document was nil")
	}

	writerBuffer := &bytes.Buffer{}
	writer := multipart.NewWriter(writerBuffer)

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
		return nil, err
	}

	if _, err := io.Copy(part, document); err != nil {
		panic(err)
	}

	if err := writer.Close(); err != nil {
		return nil, err
	}

	client := http.Client{
		Timeout: time.Duration(60) * time.Second,
	}

	response, err := client.Post(a.adapter_api_url, writer.FormDataContentType(), writerBuffer)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("response from adapter API was not okay. Got %v", response.Status)
	}

	buffer, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}

	return &buffer, nil
}

func getAdapterApiUrl() string {
	const ADAPTER_API = "http://127.0.0.1:8000/api/v1/adapt/segment"

	value := os.Getenv("ADAPTER_API_URL")
	if value == "" {
		return ADAPTER_API
	}

	return value
}
