# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Heph4estus is a TUI/CLI tool for scaling red team scanning using AWS cloud infrastructure. It uses a **worker pool model**: targets are batch-enqueued to SQS, N long-running Fargate workers poll and process them, and progress is tracked via S3 object count.

**Current state:** All modules (including nmap) run on the generic worker backend. The TUI provides a specialized nmap config/status/results flow, while other tools use the generic views. Module definitions live in YAML under `internal/modules/definitions/`. See `ARCHITECTURE.md` for details.

## Build & Run Commands

Go version: **1.26**

```bash
# Build all binaries
make build

# Build the CLI only
go build -o bin/heph ./cmd/heph

# Build generic worker Docker image for nmap
make docker-build-nmap-generic

# Deploy infrastructure for a tool
./bin/heph infra deploy --tool nmap

# Tear down infrastructure (must empty S3 bucket first)
./bin/heph infra destroy --tool nmap

# Run the TUI
./bin/heph4estus

# Run the CLI
./bin/heph nmap --file targets.txt

# Download Go dependencies
go mod tidy
```

Tests exist for cloud, infra, and TUI packages (`go test ./...`).

## Architecture

**Data flow:** TUI → SQS (batch send) → N Fargate Workers (poll loop) → S3 → TUI (progress via S3 count)

- **`cmd/heph4estus`** — TUI entry point (Bubbletea)
- **`cmd/heph`** — CLI entry point with subcommands: nmap, infra, scan, status
- **`cmd/workers/generic`** — Generic worker; runs as ECS Fargate task, polls SQS, executes any tool via module definitions, uploads results to S3
- **`internal/tui/`** — Bubbletea views: menu, settings, nmap config/status/results, generic config/status/results, deploy pipeline
- **`internal/cloud/aws/`** — Thin wrappers around AWS SDK clients (ECS, EC2, SQS, S3), each accepting a logger via constructor
  - `ecs.go` — Fargate task launching via ECS RunTask (batches of 10)
  - `ec2.go` — EC2 Spot Fleet via CreateFleet API (`capacity-optimized-prioritized`)
  - `composite.go` — `CompositeCompute` delegates RunContainer→ECS, RunSpotInstances→EC2
  - `userdata.go` — Generates bootstrap script for spot instances (Docker install → ECR pull → run → self-terminate)
- **`internal/cloud/provider.go`** — Cloud interfaces: `Storage` (Upload/Download/List/Count), `Queue` (Send/SendBatch/Receive/Delete), `Compute` (RunContainer/RunSpotInstances/GetSpotStatus), `ProgressCounter` (O(1) progress at scale)
- **`internal/infra/`** — CLI wrappers for Terraform, Docker, ECR
- **`internal/config/`** — Configuration from environment variables
- **`internal/worker/`** — Generic worker types (`Task`, `Result`), executor, error classification, jitter, template rendering
- **`internal/tools/nmap/`** — Nmap-specific logic: target parsing, scan execution (5-min timeout), data types (`ScanTask`, `ScanResult`)
- **`internal/modules/`** — Module registry and YAML-defined tool specifications
- **`internal/logger/`** — Simple logger implementing a `Logger` interface

**Terraform** (`deployments/aws/generic/`) is organized into shared modules (networking, security) and tool modules (compute, messaging, storage, spot) composed in `deployments/aws/generic/environments/dev/`.

## Key Conventions

- Go module name is `heph4estus` — imports use `heph4estus/internal/...`
- Constructor functions accept a logger for dependency injection (e.g., `NewSQSClient(logger)`)
- S3 result keys follow the pattern `scans/<tool>/<job>/results/<target>_<timestamp>.json`
- Target file format: one target per line as `<target> [nmap options]`, default options are `-sS`
- All tools use the generic worker container (`containers/generic/Dockerfile`) with tool-specific install commands injected via build args
- Nmap-specific options (timing template, DNS servers, no-rDNS) are injected producer-side into task options, not as worker env vars
