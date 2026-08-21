package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/kiran94/openitinerary-batch-processing/internal/clients"
	"github.com/kiran94/openitinerary-batch-processing/internal/storage"
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

	// Since the conversion runs within a Go routine
	// we use this channel to inspect the error
	// after the conversion is done back on the main thread.
	convertDone := make(chan error)
	go func() {
		err = client.Convert(ctx, file, filepath.Base(path), "application/pdf", writer)
		convertDone <- err
	}()

	destinationFile := "test.json"
	destination := storage.NewLocalStorage(destinationFile)
	fmt.Printf("Writing data to %v \n", destinationFile)

	copyErr := destination.Write(reader)
	convertError := <-convertDone

	if err := errors.Join(convertError, copyErr); err != nil {
		panic(err)
	}
}
