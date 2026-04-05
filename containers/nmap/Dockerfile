FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy Go module files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire codebase
COPY . .

# Build the consumer application
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/scanner ./cmd/workers/nmap

# Create a minimal production image
FROM alpine:3.19

# Install nmap and required dependencies
RUN apk add --no-cache \
    nmap \
    nmap-scripts \
    ca-certificates \
    tzdata

# Create directory for the application
WORKDIR /app

# Copy the built binary from the builder stage
COPY --from=builder /app/bin/scanner /app/scanner

# Set environment variables (defaults will be verified by entrypoint)
ENV QUEUE_URL=""
ENV S3_BUCKET=""
ENV JITTER_MAX_SECONDS=""
ENV NMAP_TIMING_TEMPLATE=""
ENV DNS_SERVERS=""
ENV NO_RDNS=""

# Add non-root user for security
RUN addgroup -S scanner && adduser -S scanner -G scanner
RUN chown -R scanner:scanner /app
USER scanner

ENTRYPOINT ["/app/scanner"]
