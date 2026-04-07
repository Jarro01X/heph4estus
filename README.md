# Heph4estus - Scaling red team tooling with cloud infrastructure

## Important Notes

- This project is designed for legitimate security testing only. Ensure you have permission to scan the targets.
- Scan costs depend on the number and duration of scans run.
- The default configuration launches Fargate tasks with 0.25 vCPU and 0.5GB memory.
- Worker count is user-configurable. At 50+ workers, automatically switches to EC2 Spot Fleet for cost savings.

## Project Overview

Heph4estus is a TUI/CLI app that handles cloud infrastructure deployment and distributed execution of red team tools. For an in-depth explanation of the architecture, roadmap, and the project as a whole please check [hephaestus.tools](https://www.hephaestus.tools).

**You provide:** cloud credentials + input files (targets, hashes). **Heph4estus handles:** infrastructure provisioning, container builds, job orchestration, result collection, and teardown.

**Built-in modules today:** `nmap`, `nuclei`, `ffuf`, `subfinder`, `httpx`, `masscan`, `gobuster`, `feroxbuster`, `dnsx`, `katana`, `gospider`, `massdns`, `dalfox`, `gowitness`. All modules run on the generic worker backend. See `ARCHITECTURE.md`, `PLAN.md`, and `IMPLEMENTATION.md` for the roadmap.

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
go build -o bin/heph ./cmd/heph
go build -o bin/heph4estus ./cmd/heph4estus
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

### 4. Run

`heph scan` and `heph nmap` automatically deploy infrastructure when needed. If matching infrastructure already exists, it is reused without redeploying.

```bash
# Nmap — auto-deploys if no nmap infra exists, reuses if it does
./bin/heph nmap --file targets.txt

# Generic target_list flow — same lifecycle behavior
./bin/heph scan --tool httpx --file targets.txt

# Generic wordlist flow
./bin/heph scan --tool ffuf --wordlist words.txt --target https://example.com/FUZZ --chunks 20

# Interactive TUI (also auto-detects existing infra)
./bin/heph4estus
```

#### Lifecycle flags

Both `heph scan` and `heph nmap` accept these lifecycle flags:

- `--no-deploy` — fail instead of deploying or redeploying (power-user / CI mode)
- `--auto-approve` — skip deploy confirmation prompts when lifecycle work is needed
- `--destroy-after` — tear down infrastructure after the run completes

```bash
# CI pipeline: deploy, scan, tear down, no prompts
./bin/heph nmap --file targets.txt --auto-approve --destroy-after

# Explicit "infra must already exist" mode
./bin/heph scan --tool httpx --file targets.txt --no-deploy
```

### 5. Explicit Infrastructure Management

`heph infra` is still available as the power-user and CI path for managing infrastructure directly:

```bash
# Deploy infrastructure for a tool
./bin/heph infra deploy --tool nmap

# Tear down (empty S3 bucket first if destroy fails)
./bin/heph infra destroy --tool nmap
```

### 6. Clean Up

Infrastructure can be destroyed after a run using `--destroy-after`, or manually:

```bash
./bin/heph infra destroy --tool nmap
./bin/heph infra destroy --tool httpx
./bin/heph infra destroy --tool ffuf
```

### Migration from dedicated nmap backend

If you previously deployed the dedicated nmap infrastructure (`deployments/aws/nmap/environments/dev`), you must destroy it before switching to the generic backend. The dedicated Terraform files have been removed from this repo, so you need to destroy using your existing local Terraform state:

```bash
# Option 1: Check out the last commit that still has the dedicated nmap Terraform,
# then destroy from there.
git stash  # if needed
git checkout <last-commit-with-dedicated-nmap>
cd deployments/aws/nmap/environments/dev
# Empty the S3 bucket first (aws s3 rm s3://<bucket> --recursive)
terraform destroy
git checkout -  # return to current branch

# Option 2: If your Terraform state is remote (S3 backend), you can run
# terraform destroy from any checkout that still has the .tf files, or
# delete the resources manually via the AWS console.
```

Then deploy generic nmap infrastructure:

```bash
./bin/heph infra deploy --tool nmap
```

Future nmap results will land under the generic job-scoped prefixes (`scans/nmap/<job>/...`).

## Development

For manual infrastructure management during development:

```bash
# Deploy generic infrastructure for any tool
./bin/heph infra deploy --tool nmap
./bin/heph infra deploy --tool httpx

# Build nmap generic worker image manually
docker build -t heph-nmap-worker \
  --build-arg RUNTIME_INSTALL_CMD="apk add --no-cache nmap nmap-scripts" \
  -f containers/generic/Dockerfile .

# Tear down (empty S3 bucket first)
./bin/heph infra destroy --tool nmap
```
