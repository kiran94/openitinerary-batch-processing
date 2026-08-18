package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/kiran94/openitinerary-batch-processing/internal/clients"
)

func main() {
	path := "/Users/kiran/Documents/projects/open_itinerary/data/example_documents/Hotel_Brisbane.pdf"

	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	http := &http.Client{
		Timeout: time.Duration(60) * time.Second,
	}
	var client clients.Adapter = clients.NewAdapterClient(http)

	converted, err := client.Convert(file, filepath.Base(path), "application/pdf")
	if err != nil {
		panic(err)
	}

	fmt.Println(string(*converted))
}
