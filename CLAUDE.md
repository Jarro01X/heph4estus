# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Heph4estus is a TUI/CLI tool for scaling red team network scanning (Nmap) using AWS cloud infrastructure. It uses a **worker pool model**: targets are batch-enqueued to SQS, N long-running Fargate workers poll and process them, and progress is tracked via S3 object count.

**Current state:** TUI with nmap flow (configure → deploy → scan → results). The full vision includes a generalized module system supporting 60+ security tools via YAML definitions, and multi-cloud support. See `ARCHITECTURE.md` for details.

## Build & Run Commands

Go version: **1.26**

```bash
# Build the CLI
go build -o bin/heph ./cmd/heph

# Build the nmap worker Docker image
docker build -t nmap-scanner -f containers/nmap/Dockerfile .

# Deploy infrastructure
cd deployments/aws/nmap/environments/dev && terraform init && terraform plan && terraform apply

# Tear down infrastructure (must empty S3 bucket first)
cd deployments/aws/nmap/environments/dev && terraform destroy

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
- **`cmd/workers/nmap`** — Runs as an ECS Fargate task inside Docker; loops polling SQS, runs `nmap`, uploads results to S3, exits when queue empty
- **`internal/tui/`** — Bubbletea views: menu, settings, nmap config/status/results, deploy pipeline
- **`internal/cloud/aws/`** — Thin wrappers around AWS SDK clients (ECS, EC2, SQS, S3), each accepting a logger via constructor
  - `ecs.go` — Fargate task launching via ECS RunTask (batches of 10)
  - `ec2.go` — EC2 Spot Fleet via CreateFleet API (`capacity-optimized-prioritized`)
  - `composite.go` — `CompositeCompute` delegates RunContainer→ECS, RunSpotInstances→EC2
  - `userdata.go` — Generates bootstrap script for spot instances (Docker install → ECR pull → run → self-terminate)
- **`internal/cloud/provider.go`** — Cloud interfaces: `Storage` (Upload/Download/List/Count), `Queue` (Send/SendBatch/Receive/Delete), `Compute` (RunContainer/RunSpotInstances/GetSpotStatus), `ProgressCounter` (O(1) progress at scale)
- **`internal/infra/`** — CLI wrappers for Terraform, Docker, ECR
- **`internal/config/`** — Loads configuration from environment variables (`QUEUE_URL` and `S3_BUCKET` for consumer)
- **`internal/tools/nmap/`** — Nmap-specific logic: target parsing, scan execution (5-min timeout), data types (`ScanTask`, `ScanResult`)
- **`internal/logger/`** — Simple logger implementing a `Logger` interface

**Terraform** (`deployments/aws/`) is organized into shared modules (networking, security) and tool-specific modules (nmap: compute, messaging, orchestration, storage) composed in `deployments/aws/nmap/environments/dev/`.

## Key Conventions

- Go module name is `heph4estus` — imports use `heph4estus/internal/...`
- Constructor functions accept a logger for dependency injection (e.g., `NewSQSClient(logger)`)
- S3 result keys follow the pattern `scans/{target}_{timestamp}.json`
- Target file format: one target per line as `<target> [nmap options]`, default options are `-sS`
- Consumer Docker image runs as non-root user `scanner` on Alpine with nmap installed
