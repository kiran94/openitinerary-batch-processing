VERSION := $(shell git describe --always --long --dirty)

GOLANGCI_LINT_VERSION := v2.13.1
GOTESTSUM_VERSION := v1.13.0
GOVULNCHECK_VERSION := v1.1.4
GORM_CLI_VERSION := v0.2.4
PROTOC_GEN_GO_VERSION := v1.36.12
PROTOC_GEN_GO_GRPC_VERSION := v1.6.2

POSTGRES_USER ?= openitinerary
POSTGRES_PASSWORD ?= openitinerary
POSTGRES_DB ?= openitinerary_batch_processing


.PHONY: build ci client_test diagram format format_check generate_oi_go_models install_proto_tools install_tools lint login_db proto_generate proto_lint test vuln api_grpc docker_compose_local_otel docker_compose_cloud docker_images_build_service docker_images_build_worker version

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

api:
	 go run .

api_grpc:
	go run . --grpc

# Live reload
api_dev:
	go tool air

worker:
	# ADAPTER_API_URL="http://0.0.0.0:8000/api/v1/adapt/segment" go run ./cmd/worker/main.go
	ADAPTER_API_URL="http://0.0.0.0:8000/api/v1/mock/segment-mock" go run ./cmd/worker/main.go

install_tools: install_proto_tools
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install gotest.tools/gotestsum@$(GOTESTSUM_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	go install gorm.io/cli/gorm@$(GORM_CLI_VERSION)
	go get -tool github.com/air-verse/air@latest

install_proto_tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GO_GRPC_VERSION)

docker_compose_local_otel:
	OTEL_COLLECTOR_CONFIG_FILE=otel-collector-config.yaml docker compose --profile local_otel up

docker_compose_cloud:
	OTEL_COLLECTOR_CONFIG_FILE=otel-collector-config-cloud.yaml docker compose up

docker_images_build_service:
	docker image build -t oibp:${VERSION} .

docker_images_build_worker:
	docker build -t oibp-worker:${VERSION} -f ./cmd/worker/Dockerfile .

version:
	@echo ${VERSION}

login_db:
	uvx pgcli "postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:5432/$(POSTGRES_DB)"

terraform_output:
	terraform -chdir=terraform output

D2 ?= d2
DIAGRAM_SOURCE := docs/io_batch_processing.d2
DIAGRAM_OUTPUT := docs/io_batch_processing.svg

diagram:
	$(D2) --layout=elk $(DIAGRAM_SOURCE) $(DIAGRAM_OUTPUT)

proto_lint:
	buf lint proto

proto_generate:
	cd proto && buf generate
