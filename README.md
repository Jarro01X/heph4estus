# Heph4estus - Scaling red team tooling with cloud infrastructure

## Important Notes

- This project is designed for legitimate security testing only. Ensure you have permission to scan the targets.
- Scan costs depend on the number and duration of scans run.
- The default configuration launches Fargate tasks with 0.25 vCPU and 0.5GB memory.
- Worker count is user-configurable. At 50+ workers, automatically switches to EC2 Spot Fleet for cost savings.

## Project Overview

Heph4estus is a TUI/CLI app that handles cloud infrastructure deployment and distributed execution of red team tools. For an in-depth explanation of the architecture, roadmap, and the project as a whole please check [hephaestus.tools](https://www.hephaestus.tools).

**You provide:** cloud credentials + input files (targets, hashes). **Heph4estus handles:** infrastructure provisioning, container builds, job orchestration, result collection, and teardown.

**Built-in modules today:** `nmap`, `nuclei`, `ffuf`, `subfinder`, `httpx`, `masscan`, `gobuster`, `feroxbuster`, `dnsx`, `katana`, `gospider`, `massdns`, `dalfox`, `gowitness`. The remaining Phase 5 work is converging `nmap` onto the generic backend; Naabu+Nmap remains planned. See `ARCHITECTURE.md`, `PLAN.md`, and `IMPLEMENTATION.md` for the roadmap.

## Requirements

- **Go 1.26+**: For building the application
- **Docker**: For building container images (managed by heph4estus)
- **Terraform 1.0+**: For infrastructure provisioning (managed by heph4estus)
- **AWS CLI**: Configured with appropriate credentials and permissions

## Quick Start

### 1. Clone and Build

```bash
git clone <repository-url>
cd heph4estus
go build -o bin/heph cmd/heph/main.go
go build -o bin/heph4estus cmd/heph4estus/main.go
```

### 2. Authenticate with AWS

```bash
aws sso login
# or configure credentials via env vars / ~/.aws/credentials
```

### 3. Create Input Files

Create a file named `targets.txt` with one target per line for `nmap` or generic `target_list` tools:

```
example.com -sV -p 80,443
10.0.0.0/24 -sS -p 22
192.168.1.1 -A
```

Format: `<target> [nmap options]`. Default options are `-sS` if none specified.

For wordlist-driven tools such as `ffuf`, create a file named `words.txt`:

```
admin
login
api
```

### 4. Deploy Infrastructure

The CLI reads existing Terraform outputs. Deploy the matching infrastructure first:

```bash
# Dedicated nmap backend
./bin/heph infra deploy --tool nmap

# Generic backend for target_list or wordlist modules
./bin/heph infra deploy --tool httpx --backend generic
# or
./bin/heph infra deploy --tool ffuf --backend generic
```

### 5. Run

Examples:

```bash
# Dedicated nmap flow
./bin/heph nmap --file targets.txt

# Generic target_list flow
./bin/heph scan --tool httpx --file targets.txt

# Generic wordlist flow
./bin/heph scan --tool ffuf --wordlist words.txt --target https://example.com/FUZZ --chunks 20

# Interactive TUI
./bin/heph4estus
```

The TUI handles deploy -> scan -> results interactively. The CLI expects the matching infrastructure to already be deployed and then handles submit -> monitor -> results.

### 6. Clean Up

Cleanup is prompted from within the TUI/CLI after results are collected, or can be run manually:

```bash
./bin/heph infra destroy --tool nmap
./bin/heph infra destroy --tool httpx --backend generic
./bin/heph infra destroy --tool ffuf --backend generic
```

## Development

For manual infrastructure management during development:

```bash
# Dedicated nmap backend
cd deployments/aws/nmap/environments/dev
terraform init && terraform plan && terraform apply

# Build and push container image manually
docker build -t nmap-scanner -f containers/nmap/Dockerfile .
ECR_REPO=<ecr_repository_url from terraform output>
ECR_REGISTRY=<registry portion of ECR_REPO>
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin $ECR_REGISTRY
docker tag nmap-scanner:latest $ECR_REPO:latest
docker push $ECR_REPO:latest

# Tear down (empty S3 bucket first)
cd deployments/aws/nmap/environments/dev
terraform destroy
```

For generic modules during development, use the CLI helper so the shared generic Terraform variables and Docker build args stay aligned with the selected tool:

```bash
./bin/heph infra deploy --tool httpx --backend generic
./bin/heph infra deploy --tool ffuf --backend generic
```
