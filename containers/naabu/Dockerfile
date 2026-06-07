FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/bin/worker ./cmd/workers/generic

RUN apk add --no-cache build-base libpcap-dev

ARG GO_INSTALL_CMD="go install github.com/projectdiscovery/naabu/v2/cmd/naabu@v2.6.1"
ENV GOBIN=/app/bin
RUN sh -c "${GO_INSTALL_CMD}"

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata libpcap nmap nmap-scripts

WORKDIR /app

COPY --from=builder /app/bin/ /app/bin/
ENV PATH="/app/bin:${PATH}"

ENV QUEUE_URL=""
ENV S3_BUCKET=""
ENV TOOL_NAME=""
ENV JITTER_MAX_SECONDS=""

RUN addgroup -S worker && adduser -S worker -G worker
RUN chown -R worker:worker /app
USER worker

ENTRYPOINT ["/app/bin/worker"]
