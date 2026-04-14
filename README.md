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

## Cloud Providers

**AWS** (default, fully integrated): SQS + S3 + ECS Fargate / EC2 Spot Fleet. Infrastructure is provisioned and destroyed automatically via Terraform.

**VPS providers** (`manual`, `hetzner`, `linode`, `scaleway`, `vultr`; provider-aware fleet manager planned): NATS JetStream + S3-compatible storage (MinIO) + Docker-over-SSH compute. Scan execution is supported when the operator provides controller endpoints and worker hosts via environment variables. PR 6.2 opens the manual/operator-managed path; `manual` is the expert/escape-hatch mode, not the flagship operator UX. PR 6.3 is the first polished provider-native VPS path via `hetzner`, and Phase 6 follow-on PRs extend the same fleet-manager model to `linode` and `vultr`. The legacy `selfhosted` selector remains accepted as a compatibility alias for `manual`.

## Requirements

- **Go 1.26+**: For building the application
- **Docker**: For building container images (managed by heph4estus)
- **Terraform 1.0+**: For infrastructure provisioning (managed by heph4estus)
- **AWS CLI**: Configured with appropriate credentials and permissions (AWS path only)

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

### 5. VPS Scan Execution (Manual/Operator-Managed Today)

The current VPS-family path is intentionally split in two:

- `manual` is the expert mode and escape hatch for operator-managed environments
- provider-native UX starts with `hetzner` in PR 6.3, then expands to `linode` and `vultr` in later Phase 6 PRs

Selfhosted scan execution works when the operator has already provisioned:

- A reachable NATS JetStream endpoint (queue)
- A reachable MinIO or S3-compatible endpoint (storage)
- Worker hosts with Docker and SSH access
- A worker image reachable by those hosts

Set the required environment variables:

```bash
# Controller endpoints
export NATS_URL="nats://controller:4222"
export S3_ENDPOINT="http://controller:9000"
export S3_REGION="us-east-1"
export S3_ACCESS_KEY="minioadmin"
export S3_SECRET_KEY="minioadmin"
export S3_PATH_STYLE="true"

# Scan runtime contract (env-driven)
export SELFHOSTED_QUEUE_ID="heph-tasks"
export SELFHOSTED_BUCKET="heph-results"

# Worker compute config
export SELFHOSTED_WORKER_HOSTS="w1.example.com,w2.example.com"
export SELFHOSTED_SSH_USER="heph"
export SELFHOSTED_SSH_KEY_PATH="$HOME/.ssh/id_ed25519"
export SELFHOSTED_DOCKER_IMAGE="controller:5000/heph-nmap-worker:latest"
# Optional: export SELFHOSTED_SSH_PORT="22"
# Optional: export NATS_STREAM="heph-tasks"
```

Run scans:

```bash
# Nmap scan on the manual/operator-managed VPS path
./bin/heph nmap --file targets.txt --cloud manual

# Generic tool scan on a named VPS provider
./bin/heph scan --tool httpx --file targets.txt --cloud hetzner
```

Host and network requirements:
- Treat each worker VM as one source-IP slot when you care about IP diversity. If you run more workers than unique hosts, some workers will share the same public IP.
- Each worker host should have a stable public IPv4 address. For IPv6 scanning, each host must also have a routable public IPv6 address.
- Provider firewalls/security groups must allow outbound IPv6 and outbound access to the controller services (NATS, MinIO, registry).
- The worker container must have a validated IPv6 egress path from inside Docker. In practice that means host networking or a verified Docker IPv6 configuration on the worker VM before relying on `-6`.
- Each host must be able to pull the worker image, reach the controller endpoints, and accept non-interactive SSH from the operator.

Scheduler rules:
- The queue is the task scheduler; the host inventory is the source-IP pool.
- The current selfhosted path is manual: Heph launches workers across `SELFHOSTED_WORKER_HOSTS`, so maximum source-IP diversity today is bounded by the number of unique hosts you supply.
- For maximum diversity today, keep `--workers` less than or equal to the number of unique worker hosts.
- The PR 6.3 target is a provider-aware fleet manager that owns host provisioning, health, public IP metadata, IPv6 capability checks, and diversity-aware placement. Its default rule should be one worker container per healthy host/public IP, with multi-worker-per-host reserved for explicit throughput mode.

What success looks like:
- Tasks enqueue to the NATS queue identified by `SELFHOSTED_QUEUE_ID`
- Workers launch over SSH on the configured hosts
- Workers read from NATS and upload results to `SELFHOSTED_BUCKET`
- `heph status --job-id <id>` can reattach using recorded job metadata

What is not yet supported:
- `heph infra deploy --cloud hetzner` polished provider-native UX (deferred to PR 6.3)
- `heph infra deploy --cloud linode` provider adapter and UX (deferred to PR 6.4)
- `heph infra deploy --cloud vultr` provider adapter and UX (deferred to PR 6.5)
- `manual` becoming a zero-config mainstream path; it remains expert mode
- Automatic controller-output consumption by scan paths (scan execution uses the env-driven contract above)

### 6. Explicit Infrastructure Management

`heph infra` is still available as the power-user and CI path for managing infrastructure directly:

```bash
# Deploy infrastructure for a tool
./bin/heph infra deploy --tool nmap

# Tear down (empty S3 bucket first if destroy fails)
./bin/heph infra destroy --tool nmap
```

### 7. Clean Up

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
