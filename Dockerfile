FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy Go module files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire codebase
COPY . .

# Build the consumer application
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/scanner ./cmd/consumer

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

# Add non-root user for security
RUN addgroup -S scanner && adduser -S scanner -G scanner
RUN chown -R scanner:scanner /app
USER scanner

# Use a direct shell command as entrypoint instead of a separate script
ENTRYPOINT [ "sh", "-c", "\
echo \"Starting nmap scanner container...\" && \
echo \"Environment variables:\" && \
echo \"QUEUE_URL: ${QUEUE_URL:-not set}\" && \
echo \"S3_BUCKET: ${S3_BUCKET:-not set}\" && \
if [ -z \"$QUEUE_URL\" ]; then \
    echo \"Error: QUEUE_URL environment variable is not set\" && \
    exit 1; \
fi && \
if [ -z \"$S3_BUCKET\" ]; then \
    echo \"Error: S3_BUCKET environment variable is not set\" && \
    exit 1; \
fi && \
echo \"Starting scanner application...\" && \
exec /app/scanner \
" ]