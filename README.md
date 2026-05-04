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

**VPS providers** (`manual`, `hetzner`, `linode`, `scaleway`, `vultr`): NATS JetStream + S3-compatible storage (MinIO) on a shared VPS runtime family. `manual` is the expert/operator-managed Docker-over-SSH path. `hetzner`, `linode`, and `vultr` are the mainstream provider-native paths: Terraform provisions the controller + workers, cloud-init boots a persistent worker service, workers self-register with the fleet manager, and scans wait for fleet readiness instead of SSH-launching ad hoc workers from the operator machine. `scaleway` remains in the shared runtime family but does not yet have a provider-native adapter. The legacy `selfhosted` selector remains accepted as a compatibility alias for `manual`.

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

Both scan commands also accept provider/runtime flags including `--cloud`, `--workers`, `--placement`, `--max-workers-per-host`, `--min-unique-ips`, `--ipv6-required`, and `--dual-stack-required`. Placement flags matter on provider-native VPS fleets: `diversity` is the default and admits one worker per healthy public IP, while `throughput` allows multiple workers per host when raw concurrency matters more than source-IP diversity.

```bash
# CI pipeline: deploy, scan, tear down, no prompts
./bin/heph nmap --file targets.txt --auto-approve --destroy-after

# Explicit "infra must already exist" mode
./bin/heph scan --tool httpx --file targets.txt --no-deploy

# VPS diversity gate: wait for 25 unique IPv4-backed workers before scanning
./bin/heph scan --tool httpx --file targets.txt --cloud hetzner --workers 25 --min-unique-ips 25
```

### 5. VPS Scan Execution

The VPS-family path is intentionally split in two:

- `manual` is the expert mode and escape hatch for operator-managed environments
- `hetzner`, `linode`, and `vultr` are the provider-native controller-plane paths

#### Manual Mode (`--cloud manual`)

Manual scan execution works when the operator has already provisioned:

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

# Generic tool scan on the manual/operator-managed VPS path
./bin/heph scan --tool httpx --file targets.txt --cloud manual
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
- The provider-native fleet manager owns host provisioning, health, public IP metadata, IPv6 capability checks, and diversity-aware placement. Its default rule is one worker container per healthy host/public IP, with multi-worker-per-host reserved for explicit throughput mode.

What success looks like:
- Tasks enqueue to the NATS queue identified by `SELFHOSTED_QUEUE_ID`
- In manual mode, Heph launches workers over SSH on the configured hosts
- Workers read from NATS and upload results to `SELFHOSTED_BUCKET`
- `heph status --job-id <id>` can reattach using recorded job metadata

Cleanup note: `manual` mode is operator-managed infrastructure, so `--destroy-after` is intentionally skipped. Use `manual` when you own the controller and worker lifecycle yourself; use `hetzner`, `linode`, or `vultr` when you want Heph to own deploy/destroy.

#### Provider-Native VPS Paths (`--cloud hetzner|linode|vultr`)

For the provider-native VPS paths, normal operator flows do not require the raw `SELFHOSTED_*` runtime contract above. The expected flow is:

```bash
# Provider-native deploy + scan
./bin/heph infra deploy --tool nmap --cloud hetzner
./bin/heph nmap --file targets.txt --cloud hetzner
```

On these paths, Terraform provisions the controller and worker VMs, cloud-init boots the persistent worker service, workers self-register with the fleet manager, and the scan waits for fleet readiness instead of SSH-launching workers from the operator machine. SSH remains a bootstrap/debug tool, not the normal orchestration model.

Provider setup:

- Hetzner: set `HCLOUD_TOKEN`
- Linode: set `LINODE_TOKEN`
- Vultr: set `VULTR_API_KEY`
- SSH public key: Heph uses `HEPH_SSH_PUBLIC_KEY`, `SSH_PUBLIC_KEY`, `HEPH_SSH_PUBLIC_KEY_PATH`, `SSH_PUBLIC_KEY_PATH`, or the first existing `~/.ssh/id_ed25519.pub` / `~/.ssh/id_rsa.pub`

```bash
export HCLOUD_TOKEN="..."
export HEPH_SSH_PUBLIC_KEY_PATH="$HOME/.ssh/id_ed25519.pub"

./bin/heph init \
  --cloud hetzner \
  --placement diversity \
  --workers 25 \
  --min-unique-ips 25 \
  --output-dir ./results \
  --cleanup-policy destroy-after

./bin/heph doctor --cloud hetzner
```

Cost and teardown posture:
- Provider-native defaults create one controller plus three worker VMs unless `--workers` on scan sets a different `worker_count` for deploy.
- VPS providers bill while VMs exist. Use `--destroy-after` for one-shot runs or `./bin/heph infra destroy --tool <tool> --cloud <provider>` after validation.
- Use `--out <dir>` or a saved output directory before destroy if you need local result copies. Results in MinIO disappear when the controller VM is destroyed.

Security posture:
- Provider-native Terraform outputs include `controller_security_mode`, service TLS/auth posture, credential generation metadata, controller certificate expiry, and NATS mTLS client certificate expiry.
- `private-auth` remains compatibility mode. `tls` encrypts NATS, MinIO, and the controller registry while keeping role-scoped credentials. `mtls` additionally requires NATS client certificates for worker/operator NATS connections.
- After deploy, run `./bin/heph doctor --cloud <provider> --tool <tool>` to report controller posture, missing TLS/auth, certificate expiry, stale credentials, and mTLS output health.
- Rotate role-scoped credentials with `./bin/heph infra rotate credentials --tool <tool> --cloud <provider> --component nats|minio|registry|all`.
- Rotate controller certificates with `./bin/heph infra rotate certs --tool <tool> --cloud <provider> --component controller|ca`.
- Phase 6 production-readiness baseline is TLS plus role-scoped rotatable credentials. MinIO and registry client-certificate mTLS are intentionally deferred unless live validation shows they are needed.

Useful provider-native fleet operations:

```bash
# Provider-specific prerequisite checks
./bin/heph doctor --cloud hetzner

# Provider-specific checks plus deployed controller security posture
./bin/heph doctor --cloud hetzner --tool nmap

# Inspect fleet health, IP diversity, placement, rollout, and reputation state
./bin/heph fleet status --tool nmap --cloud hetzner

# Repair unhealthy or quarantined workers
./bin/heph fleet reconcile --tool nmap --cloud hetzner

# Quarantine or clear a public IP
./bin/heph fleet quarantine --cloud hetzner --ip 203.0.113.10 --reason "rate limited"
./bin/heph fleet unquarantine --cloud hetzner --ip 203.0.113.10

# Canary a new worker image and promote or roll back
./bin/heph fleet rollout start --tool nmap --cloud hetzner
./bin/heph fleet rollout status --tool nmap --cloud hetzner

# Benchmark deploy/readiness/IP diversity and compare recent runs
./bin/heph bench fleet --tool nmap --cloud hetzner
./bin/heph bench history --tool nmap --cloud hetzner

# Save/inspect/recover local fleet recovery metadata
./bin/heph infra backup --tool nmap --cloud hetzner --output recovery/nmap-hetzner.json
./bin/heph infra backup inspect --from recovery/nmap-hetzner.json
./bin/heph infra recover --tool nmap --cloud hetzner --from recovery/nmap-hetzner.json --dry-run
```

What is not yet supported:
- `scaleway` provider-native deploy UX; use `manual` for operator-managed Scaleway environments today
- `manual` becoming a zero-config mainstream path; it remains expert mode
- Automatic controller-output consumption for `manual`; the manual path still uses the env-driven contract above
- MinIO or registry client-certificate mTLS; NATS mTLS strict mode is available through `controller_security_mode=mtls`
- Live provider validation coverage in this repo; real Hetzner/Linode/Vultr smoke tests still need to be run against throwaway accounts before calling Phase 6 fully production-ready

### 6. Explicit Infrastructure Management

`heph infra` is still available as the power-user and CI path for managing infrastructure directly:

```bash
# Deploy infrastructure for a tool
./bin/heph infra deploy --tool nmap

# Deploy provider-native VPS infrastructure
./bin/heph infra deploy --tool nmap --cloud hetzner

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
