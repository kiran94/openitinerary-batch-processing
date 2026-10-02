VERSION := $(shell git describe --always --long --dirty)
GO_LD_FLAGS=-ldflags "-X github.com/kiran94/openitinerary-batch-processing/internal/config.Version=$(VERSION)"

GOLANGCI_LINT_VERSION := v2.13.1
GOTESTSUM_VERSION := v1.13.0
GOVULNCHECK_VERSION := v1.1.4
GORM_CLI_VERSION := v0.2.4
PROTOC_GEN_GO_VERSION := v1.36.12
PROTOC_GEN_GO_GRPC_VERSION := v1.6.2

POSTGRES_USER ?= openitinerary
POSTGRES_PASSWORD ?= openitinerary
POSTGRES_DB ?= openitinerary_batch_processing
.PHONY: build ci client_test diagram format format_check generate_oi_go_models install_proto_tools install_tools lint login_db proto_generate proto_lint test vuln api_grpc docker_compose_local_otel docker_compose_cloud docker_images_build_service docker_images_build_worker docker_images_publish_service docker_images_publish_worker version helm_lint helm_generate helm_generate_local

build:
	go build $(GO_LD_FLAGS) ./...

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
	 go run $(GO_LD_FLAGS) .

api_grpc:
	go run $(GO_LD_FLAGS) . --grpc

# Live reload
api_dev:
	go tool air

worker:
	# ADAPTER_API_URL="http://0.0.0.0:8000/api/v1/adapt/segment" go run $(GO_LD_FLAGS) ./cmd/worker/main.go
	ADAPTER_API_URL="http://0.0.0.0:8000/api/v1/mock/segment-mock" go run $(GO_LD_FLAGS) ./cmd/worker/main.go

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
	docker image build -t oibp:${VERSION} --build-arg VERSION=${VERSION} .

docker_images_build_worker:
	docker build -t oibp-worker:${VERSION} --build-arg VERSION=${VERSION} -f ./cmd/worker/Dockerfile .

docker_images_publish_service:
	docker buildx build --push -t ${SERVICE_IMAGE}:${VERSION} -t ${SERVICE_IMAGE}:latest --build-arg VERSION=${VERSION} .

docker_images_publish_worker:
	docker buildx build --push -t ${WORKER_IMAGE}:${VERSION} -t ${WORKER_IMAGE}:latest --build-arg VERSION=${VERSION} -f ./cmd/worker/Dockerfile .

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

k8_lint:
	kubeconform -summary -verbose ./infra/k8s/

helm_lint:
	helm lint infra/helm/openitinerary-batch-processing

helm_generate:
	@helm template oibp infra/helm/openitinerary-batch-processing \
		--namespace oibp \
		--create-namespace \
		-f infra/helm/openitinerary-batch-processing/values.yaml

helm_generate_local:
	@helm template oibp infra/helm/openitinerary-batch-processing \
		--namespace oibp \
		--create-namespace \
		-f infra/helm/openitinerary-batch-processing/values.yaml \
		-f infra/helm/openitinerary-batch-processing/values-ministack.yaml

doc:
	pkgsite -open .
