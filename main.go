package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kiran94/openitinerary-batch-processing/internal/api"
)

// const ADAPTER_API = "http://127.0.0.1:8000/api/v1/adapt/segment"
const ADAPTER_API = "http://127.0.0.1:8000/api/v1/adapt/segment-mock"

func main() {
	path := "/Users/kiran/Documents/projects/open_itinerary/data/example_documents/Hotel_Brisbane.pdf"

	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	var client api.Adapter = api.NewAdapterClient(ADAPTER_API)

	converted, err := client.Convert(file, filepath.Base(path), "application/pdf")
	if err != nil {
		panic(err)
	}

	fmt.Println(string(*converted))
}
