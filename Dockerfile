# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.26.10-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

RUN mkdir -p /tmp && chmod 1777 /tmp

COPY . .

# Build a statically linked binary optimized for light containers
# - CGO_ENABLED=0 disables dynamic links to C libraries
# - TARGETOS and TARGETARCH let Buildx cross-compile without emulating Go.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-w -s -X github.com/kiran94/openitinerary-batch-processing/internal/config.Version=${VERSION}" -o oibp .

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/oibp /oibp
# For HTTP submissions whose multipart file exceeds the configured N MiB memory threshold,
# the uploaded file will spill onto the disk so this /tmp folder must exist.
COPY --from=builder /tmp /tmp

EXPOSE 8080

ENTRYPOINT [ "/oibp" ]
