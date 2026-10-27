<h1 align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/openitinerary-batch-processing-logo-dark.svg" />
    <source media="(prefers-color-scheme: light)" srcset="assets/openitinerary-batch-processing-logo.svg" />
    <img src="assets/openitinerary-batch-processing-logo.svg" alt="OpenItinerary Batch Processing" width="560" />
  </picture>
</h1>

<p align="center">
  <a href="https://github.com/kiran94/openitinerary-batch-processing/actions/workflows/build.yml"><img src="https://github.com/kiran94/openitinerary-batch-processing/actions/workflows/build.yml/badge.svg" alt="Build status" /></a>
  <a href="https://github.com/kiran94/openitinerary-batch-processing/actions/workflows/terraform.yml"><img src="https://github.com/kiran94/openitinerary-batch-processing/actions/workflows/terraform.yml/badge.svg" alt="Terraform status" /></a>
  <a href="https://github.com/kiran94/openitinerary-batch-processing/actions/workflows/protobuf.yml"><img src="https://github.com/kiran94/openitinerary-batch-processing/actions/workflows/protobuf.yml/badge.svg" alt="Protobuf status" /></a>
  <a href="https://github.com/kiran94/openitinerary-batch-processing/actions/workflows/publish-container.yml"><img src="https://github.com/kiran94/openitinerary-batch-processing/actions/workflows/publish-container.yml/badge.svg" alt="Container publishing status" /></a>
  <a href="https://github.com/kiran94/openitinerary-batch-processing/actions/workflows/k8_validate.yml"><img src="https://github.com/kiran94/openitinerary-batch-processing/actions/workflows/k8_validate.yml/badge.svg" alt="Kubernetes validation status" /></a>
  <a href="https://buf.build/openitinerary/openitinerary-batch-processing/docs/main"><img src="https://img.shields.io/badge/Buf%20Schema%20Registry-view%20schema-0C65EC" alt="View schema in the Buf Schema Registry" /></a>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> ·
  <a href="#architecture">Architecture</a> ·
  <a href="#api-reference">API Reference</a> ·
  <a href="#observability">Observability</a> ·
  <a href="#kubernetes">Kubernetes</a>
</p>

OpenItinerary Batch Processing (OIBP) is a service for converting travel booking documents into the [OpenItinerary](https://github.com/kiran94/openitinerary) standard asynchronously.

Applications can submit PDF, HTML, and email documents through HTTP or gRPC, receive a processing ticket, track its progress, and retrieve the resulting itinerary when processing completes. This moves document conversion out of the synchronous request path and provides the surrounding queueing, storage, worker, notification, and observability infrastructure needed to operate it reliably.

OIBP is part of the [OpenItinerary ecosystem](https://github.com/kiran94/openitinerary). It uses the [OpenItinerary Adapter API](https://github.com/kiran94/openitinerary#adapter-api) for document conversion and returns data represented using the open OpenItinerary standard.

## How it works

1. A client submits a PDF, HTML, or email document through the HTTP or gRPC API.
2. OIBP stores the document, creates a processing ticket, and queues the work.
3. A worker retrieves the job and sends the document to the OpenItinerary Adapter API.
4. The converted OpenItinerary data is stored and the ticket is updated with the outcome.
5. The client can track the ticket, retrieve the completed itinerary through a temporary download link, and receive an email notification.

## Architecture

![OpenItinerary Batch Processing architecture](./docs/io_batch_processing.svg)

*Solid arrows represent application and data flow. Dashed arrows represent telemetry and trace-context propagation.*

OIBP separates request handling from document conversion so that longer-running work can be processed independently and retried safely.

| Component | Responsibility |
|---|---|
| API | Accepts submissions and exposes ticket status and results through HTTP and gRPC |
| PostgreSQL | Stores ticket and processing state |
| Amazon S3 | Stores submitted documents and converted itineraries |
| Amazon SQS | Queues processing jobs and routes terminal failures to a dead-letter queue |
| Worker | Processes queued jobs and coordinates conversion, storage, and notification |
| Adapter API | Converts PDF, HTML, and email documents into OpenItinerary data |
| Amazon SES | Sends completion notifications |
| OpenTelemetry | Connects metrics, logs, and traces across the API, worker, and Adapter API |

The repository also includes Docker Compose development dependencies, Kubernetes and Helm deployment configuration, Terraform infrastructure, Grafana dashboards and alerts, and [k6 load tests](./infra/k6/README.md).

## Quick Start

OIBP runs locally while using AWS for document storage, job queueing, and email notifications.

> [!NOTE]
> The [Kubernetes setup](#kubernetes) can instead run S3, SQS, and SES locally through [MiniStack](#ministack).

### Prerequisites

- Go and Make
- Docker with Docker Compose
- AWS credentials available through the standard AWS credential chain
- An S3 bucket
- An SQS FIFO queue
- An SES configuration set and verified sender address

Export the following variables in every terminal that runs an API or worker process:

```bash
export AWS_REGION="eu-west-2"
export AWS_ACCOUNT_ID="123456789012"
export SQS_QUEUE_NAME="openitinerary-batch-processing-job-queue.fifo"

export OIBP_BUCKET="your-bucket"
export SQS_QUEUE_URL="https://sqs.${AWS_REGION}.amazonaws.com/${AWS_ACCOUNT_ID}/${SQS_QUEUE_NAME}"
export SES_NOTIFICATION_CONFIGURATION_SET_NAME="your-configuration-set"
export SES_NOTIFICATION_ADDRESS_FROM="notifications@example.com"
export OTEL_EXPORTER_OTLP_ENDPOINT="http://localhost:4318"
```

Start PostgreSQL, the Adapter API, and the local observability stack:

```bash
export OPENAI_API_KEY="your-api-key" # required for the Adapter API
make docker_compose_local_otel
```

In another terminal, start the API:

```bash
make api

# or
make api_grpc
```

After submitting a document, run a worker using the live Adapter API:

```bash
make worker
```

To exercise the processing workflow using the Adapter API's mock endpoint instead:

```bash
make worker_mock_adapter
```

`OIBP_LOG_LEVEL` defaults to `INFO`, `OIBP_TELEMETRY_ENABLED` defaults to `true`, and `OIBP_WORKER_TIMEOUT_SECONDS` defaults to `300`. Set these variables only when you need to override those defaults; for example, `OIBP_WORKER_TIMEOUT_SECONDS=180` shortens the local worker run to three minutes.

## API Reference

### HTTP

#### Submit

`POST /api/v1/submit` accepts a travel document and returns a processing ticket. The `file` and `notification_emails` multipart form fields are required. `notification_emails` must be a JSON array of email addresses.

```bash
curl \
  --form 'file=@/path/to/document.pdf;type=application/pdf' \
  --form 'notification_emails=["traveller@example.com"]' \
  http://localhost:8080/api/v1/submit
```

Supported content types are `application/pdf`, `text/html`, and `message/rfc822`.

Use the `id` from the response as the ticket identifier:

```bash
TICKET_ID="53e334b7-7519-4e97-87ba-65ecb67aafd7"
```

#### Status

`GET /api/v1/status/{id}` returns the current processing status for a ticket.

```bash
curl "http://localhost:8080/api/v1/status/${TICKET_ID}"
```

#### Result

`GET /api/v1/result/{id}` returns `204 No Content` while processing is incomplete. After successful conversion, it returns a temporary [S3 presigned URL](https://docs.aws.amazon.com/AmazonS3/latest/userguide/ShareObjectPreSignedURL.html) for the OpenItinerary result.

```bash
curl "http://localhost:8080/api/v1/result/${TICKET_ID}"
```

### gRPC

Run the gRPC service instead of the HTTP service with:

```bash
make api_grpc
```

[gRPC Server Reflection](https://grpc.io/docs/guides/reflection/) is enabled, so `grpcurl` does not require local protobuf files. The service definitions are available in [proto](./proto/).

#### Submit

```bash
go run ./cmd/grpc_client/main.go --filepath /path/to/file.pdf
```

#### Status

```bash
grpcurl \
  -plaintext \
  -d '{"ticket_id":"53e334b7-7519-4e97-87ba-65ecb67aafd7"}' \
  localhost:50051 \
  openitinerary.batch.v1.OpenItineraryBatchProcessingService/Status
```

#### Result

```bash
grpcurl \
  -plaintext \
  -d '{"ticket_id":"53e334b7-7519-4e97-87ba-65ecb67aafd7"}' \
  localhost:50051 \
  openitinerary.batch.v1.OpenItineraryBatchProcessingService/ResultLink
```

> [!NOTE]
> The protobuf definitions are also published in the [Buf Schema Registry](https://buf.build/openitinerary/openitinerary-batch-processing/docs/main).

See [Service Health](#service-health) for liveness and readiness information.

## Observability

OIBP uses [OpenTelemetry](https://opentelemetry.io/docs/) to emit metrics, logs, and traces from the API, worker, and instrumented dependencies.

For local development, the Docker Compose stack includes an OpenTelemetry Collector, Prometheus, Loki, Tempo, and Grafana:

```bash
make docker_compose_local_otel
```

Applications running outside Docker can send telemetry to the local collector with:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT="http://localhost:4318"
```

Telemetry is enabled by default and can be disabled with:

```bash
export OIBP_TELEMETRY_ENABLED=false
```

The collector can also forward telemetry to Grafana Cloud. Configure the exporter and start the cloud profile with:

```bash
export GRAFANA_OTEL_EXPORTER_OTLP_ENDPOINT="your-endpoint"
export GRAFANA_OTEL_EXPORTER_OTLP_AUTH="your-base64-encoded-credentials"

make docker_compose_cloud
```

Generate `GRAFANA_OTEL_EXPORTER_OTLP_AUTH` from the instance ID and API key provided by Grafana Cloud:

```bash
printf '%s' "INSTANCE_ID:API_KEY" | base64
```

### Metrics

OIBP emits the following standard and application-level metrics across the API and worker.

| Metric | Type | Unit | Attributes |
|---|---|---:|---|
| `http.server.request.duration` | Histogram | `s` | Standard OTel method, route and status attributes |
| `oibp.api.submissions` | Counter | `1` | `outcome`, optional `reason` |
| `oibp.api.response_write.errors` | Counter | `1` | `route` |
| `oibp.jobs.enqueued` | Counter | `1` | `content_type` |
| `oibp.jobs.terminal` | Counter | `1` | `outcome`, optional `status_reason` |
| `oibp.worker.runs` | Counter | `1` | `outcome` |
| `oibp.document.input.size` | Histogram | `By` | `content_type` |
| `oibp.adapter.convert.duration` | Histogram | `s` | `outcome` |
| `oibp.adapter.response.size` | Histogram | `By` | `content_type` |
| `oibp.storage.upload.duration` | Histogram | `s` | `purpose`, `outcome` |
| `oibp.worker.polling.duration` | Histogram | `s` | `outcome` |
| `oibp.worker.processing.duration` | Histogram | `s` | `outcome` |
| `oibp.job.end_to_end.duration` | Histogram | `s` | `outcome`, optional `status_reason` |

### Dashboards

The dashboards in [infra/grafana/dashboards](./infra/grafana/dashboards/) cover the Adapter API, the batch-processing service, and the wider OpenItinerary system. They can be provisioned in both the local Grafana stack and Grafana Cloud.

### Alerts

| Alert | Signal | Initial condition |
|---|---|---|
| Collector unavailable | `up{job="otel-collector"}` | `0` for `5m` |
| Batch HTTP failures | HTTP 5xx ratio | `>5%` for `10m` with traffic |
| Batch gRPC failures | Non-OK RPC ratio | `>5%` for `10m` with traffic |
| Adapter failures | Adapter HTTP 5xx ratio | `>5%` for `10m` with traffic |
| Terminal job failures | Failed terminal jobs | `>5%` for `10m` with completions |
| Worker failures | Failed worker runs | `>20%` for `10m` with worker activity |

Alert configuration is defined in [Grafana Alerts](./terraform/grafana_alerting.tf) and deployed to Grafana Cloud through Terraform.

### Collector Internal Telemetry

The collector buffers and retries telemetry when a downstream exporter such as Grafana Cloud is unavailable. If the sending queue reaches capacity before the exporter recovers, new telemetry may be dropped.

Internal collector metrics are enabled in [otel-collector-config-cloud.yaml](./infra/otel-collector-config-cloud.yaml) to make this behaviour observable. Inspect the key queue and export-failure metrics with:

```bash
curl --silent http://localhost:9006/metrics \
  | rg 'otelcol_exporter_(queue|send_failed|enqueue_failed)'
```

### Service Health

OIBP exposes HTTP endpoints for liveness and readiness checks:

- `/health/live` returns `200 OK` while the service is alive and accepting requests.
- `/health/ready` returns `200 OK` when the latest database readiness check succeeded, or `503 Service Unavailable` when it failed.

Database readiness is checked periodically in the background rather than during every HTTP health request.

The gRPC service implements the standard [gRPC health-checking protocol](https://grpc.io/docs/guides/health-checking/) and mirrors the same readiness state:

```bash
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check
```

## Development

Install the project development tools and run the complete Go validation suite:

```bash
make install_tools
make ci
```

Additional targets validate the protobuf definitions and Kubernetes configuration:

```bash
make proto_lint
make helm_lint
make k8_lint
```

These additional targets require Buf, Helm, and Kubeconform. Continuous integration also verifies generated protobuf code, Terraform formatting and validation, and container builds.

## Kubernetes

The Helm chart in [infra/helm/openitinerary-batch-processing](./infra/helm/openitinerary-batch-processing/) deploys the API, worker, Adapter API, PostgreSQL, and OpenTelemetry Collector.

The base values connect OIBP to AWS services. For local development, the MiniStack values add locally emulated S3, SQS, and SES services to the cluster.

### Local cluster

Prerequisites: Docker, Minikube, kubectl, and Helm.

Create an isolated Minikube profile and configure the `oibp` namespace:

```bash
export OIBP_KUBE_CONTEXT="oibp"

minikube start --profile "$OIBP_KUBE_CONTEXT" --driver docker

kubectl config use-context "$OIBP_KUBE_CONTEXT"
kubectl create namespace oibp
kubectl config set-context --current --namespace oibp
```

Optionally enable resource metrics:

```bash
minikube --profile "$OIBP_KUBE_CONTEXT" addons enable metrics-server
```

Verify the cluster:

```bash
kubectl config current-context
kubectl cluster-info
kubectl get nodes --output wide
```

### Using local images

The Helm chart uses the published GHCR images by default. When testing local API or worker changes, build the images and load them into Minikube:

```bash
IMAGE_TAG="$(make version)"

make docker_images_build_service
make docker_images_build_worker

minikube --profile "$OIBP_KUBE_CONTEXT" image load "oibp:${IMAGE_TAG}"
minikube --profile "$OIBP_KUBE_CONTEXT" image load "oibp-worker:${IMAGE_TAG}"
```

Verify that Minikube can see them:

```bash
minikube --profile "$OIBP_KUBE_CONTEXT" image ls
```

The subsequent Helm installation can override the API and worker image names and tags when these local builds are required.

### Secrets

The Helm chart expects application, AWS, and OpenTelemetry credentials to be created outside the release. Create `infra/k8s/secret.yaml` and populate the values needed for your deployment. This path is already excluded by `.gitignore`.

- `oibp-secret` configures PostgreSQL.
- `oibp-secret-otel` configures the Grafana Cloud exporter when telemetry is enabled.
- `oibp-secret-aws` contains credentials for real AWS services.
- `oibp-secret-aws-ministack` contains non-production credentials used by the local MiniStack deployment.

<details>

<summary>secret.yaml</summary>

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: oibp-secret
  namespace: oibp
  labels:
    app.kubernetes.io/part-of: openitinerary
type: Opaque
stringData:
  POSTGRES_PASSWORD: "openitinerary" # example
  DATABASE_DSN: "host=postgres user=openitinerary password=openitinerary dbname=openitinerary port=5432 sslmode=disable" # example
---
apiVersion: v1
kind: Secret
metadata:
  name: oibp-secret-otel
  namespace: oibp
type: Opaque
stringData:
  OTEL_EXPORTER_OTLP_ENDPOINT:
  OTEL_EXPORTER_OTLP_HEADERS:
---
apiVersion: v1
kind: Secret
metadata:
  name: oibp-secret-aws
  namespace: oibp
type: Opaque
stringData:
  AWS_ACCESS_KEY_ID:
  AWS_SECRET_ACCESS_KEY:
  SES_NOTIFICATION_ADDRESS_FROM:
---
apiVersion: v1
kind: Secret
metadata:
  name: oibp-secret-aws-ministack
  namespace: oibp
type: Opaque
stringData:
  AWS_ACCESS_KEY_ID: test
  AWS_SECRET_ACCESS_KEY: test
  SES_NOTIFICATION_ADDRESS_FROM: notifications@example.com
```

</details>

### Install the chart

Apply the external secrets before installing the Helm release:

```bash
kubectl apply --filename ./infra/k8s/secret.yaml
```

Install OIBP with locally emulated AWS services:

```bash
helm upgrade --install oibp \
  ./infra/helm/openitinerary-batch-processing \
  --namespace oibp \
  --values ./infra/helm/openitinerary-batch-processing/values.yaml \
  --values ./infra/helm/openitinerary-batch-processing/values-ministack.yaml \
  --set telemetry.enabled=false \
  --wait \
  --timeout 5m
```

To connect to real AWS services, omit `values-ministack.yaml`.

To export telemetry to Grafana Cloud, omit `--set telemetry.enabled=false` and populate `oibp-secret-otel`.

If you loaded local images into Minikube, override `api.image`, `api.image_tag`, `worker.image`, and `worker.image_tag` during installation.

Verify the release:

```bash
helm status oibp --namespace oibp
kubectl get pods,services,deployments,persistentvolumeclaims,endpointslices
```

### Access the API

Forward the API service to the local machine:

```bash
kubectl port-forward service/oibp-service 8080:8080
```

The HTTP API is then available at `http://localhost:8080`.

### Run a worker manually

The Helm chart schedules workers using a Kubernetes CronJob. To process work immediately, create an ad hoc job from it:

```bash
OIBP_ADHOC_JOB="oibp-worker-adhoc-check-$(date +%H%M%S)"

kubectl create job \
  --from cronjob/oibp-worker \
  "$OIBP_ADHOC_JOB"

kubectl logs --follow "job/$OIBP_ADHOC_JOB"
```

### MiniStack

[MiniStack](https://ministack.org/) provides local S3, SQS, and SES services inside the Kubernetes cluster.

The base `values.yaml` connects to AWS. Applying `values-ministack.yaml` overrides the AWS endpoint, queue URL, S3 path-style setting, and Secret name for local development.

> [!NOTE]
> MiniStack state is disposable. Disabling or uninstalling the Helm release can remove its chart-managed volume and locally emulated AWS data.

Forward the MiniStack gateway in one terminal:

```bash
kubectl port-forward service/ministack 4566:4566
```

In another terminal, inspect the local services:

```bash
export AWS_ENDPOINT_URL="http://localhost:4566"
export AWS_ACCESS_KEY_ID="test"
export AWS_SECRET_ACCESS_KEY="test"
export AWS_REGION="eu-west-2"
export MINISTACK_QUEUE_URL="${AWS_ENDPOINT_URL}/000000000000/openitinerary-batch-processing-job-queue.fifo"

aws s3 ls

aws sqs get-queue-attributes \
  --no-cli-pager \
  --queue-url "$MINISTACK_QUEUE_URL" \
  --attribute-names ApproximateNumberOfMessages ApproximateNumberOfMessagesNotVisible ApproximateNumberOfMessagesDelayed \
  --query Attributes \
  --output table

curl --silent "${AWS_ENDPOINT_URL}/_ministack/ses/messages" | jq
```
