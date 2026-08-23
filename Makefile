GOLANGCI_LINT_VERSION := v2.13.1
GOTESTSUM_VERSION := v1.13.0
GOVULNCHECK_VERSION := v1.1.4

.PHONY: build ci client_test format format_check generate_oi_go_models install_tools lint test vuln

build:
	go build ./...

test:
	gotestsum ./... -- -race

lint:
	golangci-lint run ./...

format:
	golangci-lint fmt

format_check:
	golangci-lint fmt --diff

vuln:
	govulncheck ./...

ci: format_check lint build test vuln

generate_oi_go_models:
	go-jsonschema -p openitinerary internal/openitinerary/0.1/itinerary.schema.json -o internal/openitinerary/0.1/itinerary.gen.go

client_test:
	 ADAPTER_API_URL="http://127.0.0.1:8000/api/v1/adapt/segment-mock" go run ./main.go

install_tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install gotest.tools/gotestsum@$(GOTESTSUM_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	go install gorm.io/cli/gorm@latest


login_db:
	uvx pgcli "postgres://openitinerary:openitinerary@localhost:5432/openitinerary_batch_processing"
