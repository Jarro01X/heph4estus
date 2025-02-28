FROM golang:1.21-alpine

# Install nmap and required dependencies
RUN apk add --no-cache \
    nmap \
    nmap-scripts \
    bash \
    ca-certificates

# Set up proper Go working directory
WORKDIR /app

# Copy the entire module
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy our application code
COPY cmd/consumer/main.go ./

# Build the Go application
RUN go build -v -o scanner main.go

# Set environment variables
ENV QUEUE_URL=""
ENV S3_BUCKET=""

# Directly use CMD instead of entrypoint.sh (LF issue)
CMD /bin/sh -c 'echo "Starting nmap scanner container..."; \
    echo "Environment variables:"; \
    echo "QUEUE_URL: ${QUEUE_URL:-not set}"; \
    echo "S3_BUCKET: ${S3_BUCKET:-not set}"; \
    if [ -z "$QUEUE_URL" ]; then \
        echo "Error: QUEUE_URL environment variable is not set"; \
        exit 1; \
    fi; \
    if [ -z "$S3_BUCKET" ]; then \
        echo "Error: S3_BUCKET environment variable is not set"; \
        exit 1; \
    fi; \
    echo "Starting scanner application..."; \
    ./scanner'