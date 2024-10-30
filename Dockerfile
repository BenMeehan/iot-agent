# Use an official Golang image as a builder for building the binary
FROM golang:1.23-alpine AS builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

WORKDIR /app

COPY . .

# Build the Go application
RUN go mod download
RUN go build -o agent ./cmd/agent/main.go

# Start a new stage from scratch for a minimal container
FROM alpine:latest

WORKDIR /root/

# Copy certificates and configurations from the builder stage
COPY --from=builder /app/certs ./certs
COPY --from=builder /app/configs ./configs
COPY --from=builder /app/secrets ./secrets

COPY --from=builder /app/agent .

EXPOSE 8080

CMD ["./agent"]
