# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.26.9-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

# Build a statically linked binary optimized for light containers
# - CGO_ENABLED=0 disables dynamic links to C libraries
# - TARGETOS and TARGETARCH let Buildx cross-compile without emulating Go.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-w -s -X github.com/kiran94/openitinerary-batch-processing/internal/config.Version=${VERSION}" -o oibp-worker ./cmd/worker/main.go

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/oibp-worker /oibp-worker

ENTRYPOINT [ "/oibp-worker" ]
