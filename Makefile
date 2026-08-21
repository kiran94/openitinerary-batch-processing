
test:
	gotestsum ./... -- -race

generate_oi_go_models:
	go-jsonschema -p openitinerary internal/openitinerary/0.1/itinerary.schema.json -o internal/openitinerary/0.1/itinerary.gen.go

client_test:
	 ADAPTER_API_URL="http://127.0.0.1:8000/api/v1/adapt/segment-mock" go run ./main.go

build:
	go build ./...

install_tools:
	go install gotest.tools/gotestsum@latest