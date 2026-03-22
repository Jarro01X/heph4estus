FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy Go module files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire codebase
COPY . .

# Build the generic worker binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/worker ./cmd/workers/generic

# Install Go-based tools (e.g. nuclei, httpx) into /app/bin
ARG GO_INSTALL_CMD="true"
ENV GOBIN=/app/bin
RUN sh -c "${GO_INSTALL_CMD}"

# Create a minimal production image
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

# Install system-packaged tools (e.g. nmap, masscan)
ARG RUNTIME_INSTALL_CMD="true"
RUN sh -c "${RUNTIME_INSTALL_CMD}"

WORKDIR /app

# Copy worker binary and any Go-installed tool binaries from builder
COPY --from=builder /app/bin/ /app/bin/
ENV PATH="/app/bin:${PATH}"

# Runtime environment variables
ENV QUEUE_URL=""
ENV S3_BUCKET=""
ENV TOOL_NAME=""
ENV JITTER_MAX_SECONDS=""

# Add non-root user for security
RUN addgroup -S worker && adduser -S worker -G worker
RUN chown -R worker:worker /app
USER worker

ENTRYPOINT ["sh", "-c", "exec /app/bin/worker"]
