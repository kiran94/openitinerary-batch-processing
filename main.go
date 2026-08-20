package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/kiran94/openitinerary-batch-processing/internal/clients"
)

func main() {
	path := "/Users/kiran/Documents/projects/open_itinerary/data/example_documents/Hotel_Brisbane.pdf"
	ctx := context.Background()

	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	http := &http.Client{
		Timeout: time.Duration(60) * time.Second,
	}
	var client clients.Adapter = clients.NewAdapterClient(http)

	reader, writer := io.Pipe()
	defer reader.Close()

	go func() {
		err = client.Convert(ctx, file, filepath.Base(path), "application/pdf", writer)
		if err != nil {
			panic(err)
		}
	}()

	converted := &bytes.Buffer{}
	if _, err := io.Copy(converted, reader); err != nil {
		panic(err)
	}

	fmt.Println(converted.String())
}
