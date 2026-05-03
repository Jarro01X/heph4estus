FROM golang:1.26-alpine AS builder

WORKDIR /app

# Copy Go module files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire codebase
COPY . .

# Build the generic worker binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/worker ./cmd/workers/generic

# Install Go-based tools (e.g. nuclei, httpx) into /app/bin.
# GO_INSTALL_CMD is used by the runtime deploy path; TOOL_INSTALL_CMD is a
# Makefile-friendly compatibility arg for per-tool image targets.
ARG GO_INSTALL_CMD=""
ARG TOOL_INSTALL_CMD=""
ENV GOBIN=/app/bin
RUN if [ -n "${GO_INSTALL_CMD}" ]; then sh -c "${GO_INSTALL_CMD}"; fi; \
    case "${TOOL_INSTALL_CMD}" in \
        go\ install\ *) sh -c "${TOOL_INSTALL_CMD}" ;; \
        *) true ;; \
    esac

# Create a minimal production image
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata

# Install system-packaged tools (e.g. nmap, masscan). Runtime deploys use
# RUNTIME_INSTALL_CMD; per-tool Make targets may pass apk commands through
# TOOL_INSTALL_CMD.
ARG RUNTIME_INSTALL_CMD=""
ARG TOOL_INSTALL_CMD=""
RUN if [ -n "${RUNTIME_INSTALL_CMD}" ]; then sh -c "${RUNTIME_INSTALL_CMD}"; fi; \
    case "${TOOL_INSTALL_CMD}" in \
        ""|go\ install\ *) true ;; \
        *) sh -c "${TOOL_INSTALL_CMD}" ;; \
    esac

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

ENTRYPOINT ["/app/bin/worker"]
