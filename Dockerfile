FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY . .

# Build a statically linked binary optimized for light containers
# - CGO_ENABLED=0 disables dynamic links to C libraries
# - GOOS=linux ensures it targets the Linux container OS
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o oibp .

FROM scratch

COPY --from=builder /app/oibp /oibp

EXPOSE 8080

ENTRYPOINT [ "/oibp" ]