package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kiran94/openitinerary-batch-processing/internal/api"
)

func main() {
	path := "/Users/kiran/Documents/projects/open_itinerary/data/example_documents/Hotel_Brisbane.pdf"

	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	var client api.Adapter = api.NewAdapterClient()

	converted, err := client.Convert(file, filepath.Base(path), "application/pdf")
	if err != nil {
		panic(err)
	}

	fmt.Println(string(*converted))
}
