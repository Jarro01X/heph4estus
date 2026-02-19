# Heph4estus - Scaling red team tooling with cloud infrastructure

## Important Notes

- This project is designed for legitimate security testing only. Ensure you have permission to scan the targets.
- Scan costs depend on the number and duration of scans run.
- The default configuration launches Fargate tasks with 0.25 vCPU and 0.5GB memory.
- Max concurrency at the moment is 10 ECS Tasks.

## Project Overview

Heph4estus is a TUI/CLI app that handles cloud infrastructure deployment and distributed execution of red team tools. For an in-depth explanation of the architecture, roadmap, and the project as a whole please check [hephaestus.tools](https://www.hephaestus.tools).

**You provide:** cloud credentials + input files (targets, hashes). **Heph4estus handles:** infrastructure provisioning, container builds, job orchestration, result collection, and teardown.

**Planned tools:** Hashcat (distributed GPU cracking across EC2 spot instances, with rules support), Naabu+Nmap pipeline (fast port discovery then deep scan). See `ARCHITECTURE.md` for the full roadmap.

## Requirements

- **Go 1.21+**: For building the application
- **Docker**: For building container images (managed by heph4estus)
- **Terraform 1.0+**: For infrastructure provisioning (managed by heph4estus)
- **AWS CLI**: Configured with appropriate credentials and permissions

## Quick Start

### 1. Clone and Build

```bash
git clone <repository-url>
cd heph4estus
go build -o bin/heph-cli cmd/heph-cli/main.go
```

### 2. Authenticate with AWS

```bash
aws sso login
# or configure credentials via env vars / ~/.aws/credentials
```

### 3. Create a Targets File

Create a file named `targets.txt` with one target per line:

```
example.com -sV -p 80,443
10.0.0.0/24 -sS -p 22
192.168.1.1 -A
```

Format: `<target> [nmap options]`. Default options are `-sS` if none specified.

### 4. Run

```bash
# CLI (handles deploy → scan → results automatically)
./bin/heph-cli nmap --file targets.txt

# Or launch the TUI for interactive use
./bin/heph4estus
```

The tool will:
1. Show you a Terraform plan for the required infrastructure
2. Wait for your approval
3. Deploy the infrastructure and push container images
4. Submit your scan job and monitor progress
5. Collect and display results

### 5. Clean Up

Cleanup is prompted from within the TUI/CLI after results are collected, or can be run manually:

```bash
./bin/heph-cli infra destroy --tool nmap
```

## Development

For manual infrastructure management during development:

```bash
# Deploy infrastructure manually
cd deployments/aws/nmap/environments/dev
terraform init && terraform plan && terraform apply

# Build and push container image manually
docker build -t nmap-scanner -f containers/nmap/Dockerfile .
ECR_REPO=<ecr_repository_url from terraform output>
ECR_REGISTRY=<registry portion of ECR_REPO>
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin $ECR_REGISTRY
docker tag nmap-scanner:latest $ECR_REPO:latest
docker push $ECR_REPO:latest

# Run CLI directly with a state machine ARN
export STATE_MACHINE_ARN=<state_machine_arn from terraform output>
./bin/heph-cli -file targets.txt

# Tear down (empty S3 bucket first)
cd deployments/aws/nmap/environments/dev
terraform destroy
```
