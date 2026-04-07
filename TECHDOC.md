# Heph4estus Technical Documentation

Heph4estus is a TUI/CLI platform for scaling red team network scanning using cloud infrastructure. Named after the Greek god of the forge, it turns security tools into distributed, fault-tolerant cloud workloads.

## Architecture

### Core Design

Heph4estus uses a **producer-consumer worker pool** model. The operator's machine (laptop, CI runner, etc.) is the producer — it enqueues work to a cloud message queue, launches N ephemeral workers, and monitors progress. Workers are stateless containers that poll the queue, execute a scan, upload results to object storage, and exit when the queue is empty.

```
                    ┌──────────────┐
                    │   Operator   │
                    │  (TUI/CLI)   │
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
              ▼            ▼            ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │  Message  │ │  Object  │ │ Compute  │
        │  Queue    │ │ Storage  │ │ (ECS/EC2)│
        │  (SQS)   │ │  (S3)    │ │          │
        └────┬─────┘ └────▲─────┘ └────┬─────┘
             │             │            │
             │        ┌────┴────┐       │
             └───────►│ Workers │◄──────┘
                      │  (1..N) │
                      └─────────┘
```

This architecture has several advantages over SSH-based orchestration tools like Axiom/Ax:

- **No controller bottleneck** — workers pull work independently, no central coordinator pushing tasks over SSH
- **Built-in fault tolerance** — if a worker dies, its unfinished message returns to the queue via visibility timeout and another worker picks it up
- **Linear scaling** — adding workers adds throughput; the queue and storage handle the coordination
- **Cost-efficient** — workers are ephemeral (Fargate tasks or spot instances), infrastructure is deployed on demand and destroyed after each job

### Technology Stack

| Layer | Technology |
|-------|-----------|
| Language | Go 1.26 |
| TUI Framework | Bubbletea v2 (Elm architecture) |
| TUI Styling | Lipgloss v2 |
| TUI Components | Bubbles v2 (text inputs, lists, viewports) |
| Cloud SDK | AWS SDK for Go v2 |
| Infrastructure | Terraform (modular, per-tool) |
| Containers | Docker (Alpine-based, non-root) |
| Message Queue | AWS SQS (with DLQ for poison pills) |
| Object Storage | AWS S3 (versioned, encrypted, lifecycle policies) |
| Compute | AWS ECS Fargate + EC2 Spot Fleet |

### Cloud Interfaces

All cloud operations go through abstract interfaces defined in `internal/cloud/provider.go`:

```
Provider    ──►  Storage   (Upload, Download, List, Count)
            ──►  Queue     (Send, SendBatch, Receive, Delete)
            ──►  Compute   (RunContainer, RunSpotInstances, GetSpotStatus)
```

The AWS implementation lives in `internal/cloud/aws/`. This abstraction layer is designed for future multi-cloud support — a selfhosted provider (NATS + S3-compatible storage + Docker on VMs) and Big 3 providers (GCP, Azure) are planned.

### Compute Modes

Workers run in one of two compute modes, selected automatically or by the operator:

- **Fargate** (default, < 50 workers) — serverless containers, no VM management, pay per second
- **EC2 Spot Fleet** (auto-selected at 50+ workers) — 60-90% cheaper than Fargate, uses `CreateFleet` API with `capacity-optimized-prioritized` allocation strategy

The `CompositeCompute` type delegates `RunContainer` to ECS and `RunSpotInstances` to EC2, so the rest of the codebase doesn't need to know which mode is in use.

### Data Flow

**Scan lifecycle (TUI path):**

```
Config Form ──► Deploy Pipeline ──► Status Monitor ──► Results Viewer
    │               │                    │                   │
    │          Terraform init       Enqueue targets     Download from S3
    │          Terraform plan       Launch workers      Paginated display
    │          Terraform apply      Poll S3 count       Destroy infra
    │          Docker build
    │          ECR push
    │
    ▼
  DeployConfig ──► InfraOutputs ──► Worker Env Vars
```

**Scan lifecycle (CLI path):**

```
heph nmap --file targets.txt [flags]
    │
    ├── Read terraform outputs (queue URL, bucket, cluster)
    ├── Parse targets from file
    ├── Enqueue to SQS (batches of 10)
    ├── Launch workers (Fargate or Spot)
    ├── Poll S3 for result count
    └── Output results (text or JSON)
```

**Worker loop:**

```
Start ──► Receive from SQS ──► Parse ScanTask ──► Apply jitter
              │                                        │
              │ (empty)                          Inject flags (-T, --dns-servers, -n)
              │                                        │
              ▼                                   Run nmap (5min timeout)
            Exit                                       │
                                                 Classify error
                                                       │
                                          ┌────────────┼────────────┐
                                          │            │            │
                                       Success     Transient    Permanent
                                          │            │            │
                                     Upload S3    Leave msg    Upload error
                                     Delete msg   (SQS retry)  Delete msg
```

## Project Structure

```
heph4estus/
├── cmd/
│   ├── heph4estus/          TUI entry point (Bubbletea)
│   ├── heph/                CLI entry point (subcommands: nmap, infra, scan, status)
│   └── workers/nmap/        Nmap worker binary (runs inside Docker/ECS)
├── containers/
│   └── nmap/                Dockerfile for nmap worker (Alpine + nmap + Go binary)
├── deployments/aws/
│   ├── shared/
│   │   ├── networking/      VPC, subnets, NAT Gateway, security groups
│   │   └── security/        IAM roles (ECS execution, task, Step Functions)
│   └── nmap/
│       ├── compute/         ECS cluster, Fargate task definition, ECR repo
│       ├── messaging/       SQS queue + dead-letter queue
│       ├── storage/         S3 bucket (encrypted, versioned, lifecycle)
│       ├── spot/            EC2 spot instance profile, AMI lookup
│       ├── orchestration/   Step Functions state machine
│       └── environments/dev/  Module composition for dev environment
├── internal/
│   ├── cloud/
│   │   ├── provider.go      Cloud interfaces (Storage, Queue, Compute)
│   │   ├── aws/             AWS implementations (S3, SQS, ECS, EC2, SFN)
│   │   └── mock/            Mock provider for testing
│   ├── config/              Environment variable configuration
│   ├── infra/               CLI wrappers for Terraform, Docker, ECR
│   ├── jobs/                Job runner interface (planned Step Functions integration)
│   ├── logger/              Simple logger with Info/Error/Fatal
│   ├── tools/nmap/          Nmap-specific logic:
│   │   ├── scanner.go         Target parsing, scan execution
│   │   ├── portparse.go       Port spec parsing and splitting
│   │   ├── errors.go          Error classification (transient vs permanent)
│   │   ├── jitter.go          Pre-scan random delay (crypto/rand)
│   │   └── models.go          ScanTask, ScanResult types
│   └── tui/
│       ├── app.go             Root Bubbletea model, view switching
│       ├── core/              Shared types, styles ("Forge Theme"), stream writer
│       └── views/
│           ├── menu/          Main menu with ASCII art title
│           ├── settings/      AWS credentials and region display
│           ├── nmap/          Config form, status monitor, results viewer
│           └── deploy/        Infrastructure deploy pipeline (Terraform + Docker)
```

## Current State

**Phases 1-4 complete. Phase 5 in progress (7/9 PRs done — target-list generic TUI/CLI wiring and wordlist/fuzzer UX are shipped; nmap backend convergence remains).**

Heph4estus currently supports nmap scanning end-to-end through both TUI and CLI interfaces, with full AWS infrastructure lifecycle management. The generalized module system is built and validated — nmap can run through the generic worker and generic Terraform infrastructure.

### What works today

- **TUI**: Interactive flow from config form through infrastructure deployment, scan monitoring, and paginated results with infrastructure teardown
- **CLI**: `heph nmap --file targets.txt` with flags for workers, compute mode, DNS servers, timing, jitter, reverse DNS, output format
- **CLI (generic target-list tools)**: `heph scan --tool httpx --file targets.txt` and the same path for other built-in `target_list` modules
- **CLI (generic wordlist tools)**: `heph scan --tool ffuf --wordlist words.txt --target https://example.com/FUZZ --chunks 20` and the same path for `gobuster` and `feroxbuster`
- **Infrastructure**: `heph infra deploy --tool nmap` deploys generic infrastructure for nmap (same backend as all other tools)
- **Nmap scanning**: Worker pool model with SQS message queue, S3 result storage, and automatic progress tracking
- **Job-scoped result storage**: Scan results and artifacts are namespaced per job under `scans/{tool}/{job_id}/...`
- **Port splitting**: Target-ports mode splits port ranges across multiple workers for faster scanning of large targets
- **Scan hardening**: Retry logic with error classification, pre-scan jitter, timing templates, DNS server passthrough, reverse DNS disable
- **Compute flexibility**: Auto-selection between Fargate and EC2 Spot Fleet based on worker count (threshold: 50)
- **Spot instances**: `CreateFleet` API with capacity-optimized allocation, auto-generated UserData for Docker bootstrap and self-termination
- **Module system**: 14 YAML tool definitions, generic worker binary, generic Terraform module, generic Dockerfile with build args, argv-style module execution, pinned tool versions

### What's built

| Component | Status |
|-----------|--------|
| TUI (nmap + generic module flows) | Complete |
| CLI (nmap, generic scan, infra deploy/destroy) | Complete |
| AWS cloud provider (S3, SQS, ECS, EC2) | Complete |
| Nmap worker with retry logic | Complete |
| Port parsing and splitting | Complete |
| Scan hardening (jitter, timing, DNS, rDNS) | Complete |
| EC2 Spot Fleet support | Complete |
| Terraform modules (networking, security, compute, messaging, storage, spot) | Complete |
| Module definitions (14 YAML tools, registry, validation) | Complete |
| Generic worker binary (template commands, S3 I/O) | Complete |
| Generic Terraform module (parameterized by tool_name) | Complete |
| Generic Dockerfile (multi-stage, build-arg install) | Complete |
| Nmap converged onto generic backend (dedicated runtime retired) | Complete |
| Generic job scoping, exact progress counting, and `OutputKey` population | Complete |
| Generic execution hardening (`exec` argv templates, explicit shell opt-in) | Complete |
| Unit tests across all packages | Complete |

### What's not built yet

| Component | Phase |
|-----------|-------|
| Selfhosted/VPS provider (NATS, Hetzner) | 6 |
| Per-tool image automation, large-wordlist scaling, and richer tool-specific formatters | 7 |
| Naabu + Nmap combined scanning pipeline | 8 |
| Result export and pipeline integration | 9 |
| Dual-stack VPC, multi-NAT IP diversity | 10 |

## Roadmap

### Phase 5: Generalized Module System

The core insight: ~90% of security tools follow the same "input file -> run command -> output file" pattern. Instead of building custom Go workers and Terraform for each tool, tools are defined as YAML module definitions and run through a generic worker.

**Module definition example:**

```yaml
name: nuclei
description: Template-based vulnerability scanner
exec: ["nuclei", "-l", "{{input}}", "-o", "{{output}}", "-j"]
input_type: target_list
output_ext: jsonl
install_cmd: "go install github.com/projectdiscovery/nuclei/v3/cmd/nuclei@v3.7.1"
default_cpu: 256
default_memory: 512
timeout: 10m
tags: [scanner, vuln]
```

**What this phase delivers:**

- `internal/modules/` — Module definition parser, validation, embedded registry with `//go:embed`
- `cmd/workers/generic/` — One worker binary that loads any module definition, templates the command, and handles the full queue-execute-upload lifecycle
- `containers/generic/Dockerfile` — Multi-stage build with `TOOL_INSTALL_CMD` build arg (one Dockerfile, many tool images)
- `deployments/aws/generic/` — Parameterized Terraform module (tool name, CPU, memory, concurrency)
- Nmap fully converged onto the generic backend — dedicated runtime retired
- Generic runtime hardened with job-scoped storage, exact progress counting, explicit artifact references, and argv-first execution
- Nmap retains its specialized planner, CLI flags, and TUI views while running on the same generic worker/infrastructure as all other tools

**Built-in modules at launch:** nmap, nuclei, ffuf, subfinder, httpx, masscan, gobuster, feroxbuster, dnsx, katana, gospider, massdns, dalfox, gowitness

### Phase 6: Selfhosted Provider

Prioritized immediately after the module system — this is the killer feature for adoption. One Go implementation covering any VPS provider with VMs and S3-compatible object storage. Hetzner VPS instances are significantly cheaper than AWS Fargate, and each VM gets its own public IP (free IP diversity).

- **Queue**: NATS JetStream (ack-wait = visibility timeout, max-deliver = DLQ)
- **Storage**: MinIO (default, runs on controller VM alongside NATS — zero external dependencies, works on any VPS provider). Operators can optionally point at any S3-compatible API (Hetzner Object Storage, Backblaze B2, Cloudflare R2) via custom endpoint config
- **Compute**: SSH into provisioned VMs, run Docker containers (same images as AWS)
- **Terraform**: `deployments/hetzner/` module (worker VMs, controller VM with NATS + MinIO, networking)
- **Coverage**: Hetzner, Linode, Scaleway, Vultr, and any provider with VMs

The controller VM (~$4.50/mo on Hetzner cx21) replaces SQS + S3 + ECR:

```
Controller VM:
  ├── NATS JetStream  (replaces SQS)
  ├── MinIO            (replaces S3)
  └── Docker           (replaces ECR — workers pull images from controller)
```

### Phase 7: Tool Expansion

With the generic module system in place, adding a tool is mostly a YAML definition + a container build command, plus scale-oriented handling where certain tool classes need it.

- Per-tool container images built from the generic Dockerfile
- Scale-oriented wordlist distribution improvements for very large fuzzing inputs
- Tool-specific result formatters (nmap XML, nuclei JSONL, ffuf JSON -> structured tables)

### Phase 8: Naabu + Nmap Pipeline

Uses naabu's `-nmap-cli` flag to run port discovery and deep scanning in a single worker pass. No multi-phase orchestration, no Lambda bridge. One worker runs `naabu -host <target> -nmap-cli 'nmap <flags>'`, combining fast port discovery with deep service detection.

Two modes:
- **Combined** (default): naabu discovers ports and pipes them into nmap within the same process
- **Discovery-only**: naabu outputs JSON (open ports per target), useful for feeding into other tools

### Phase 9: Result Export & Pipeline Integration

Red teamers need to pipe results into reporting tools and other scanners.

- `heph results list|download|export` subcommands
- `--format json|csv|jsonl` for machine-readable output
- Pipe-friendly: `heph results export --job <id> --format jsonl | jq '.open_ports[]'`
- Tool-specific result formatters (nmap XML -> JSON/CSV, naabu JSON -> CSV)

### Phase 10: Networking Enhancements

Addresses the single-NAT-IP problem — scans from one IP get rate-limited by SOC rules and AWS itself.

- **Dual-stack VPC with EIGW**: IPv6 egress via Egress-only Internet Gateway (free, no hourly charge like NAT Gateway)
- **Multi-NAT Gateway**: One NAT per AZ for IP diversity (3 AZs = 3 unique source IPs, at additional cost)
- **TUI/CLI controls**: IPv6 toggle, multi-NAT toggle, display current NAT IPs
- Note: Selfhosted providers get IP diversity for free — each VM has its own public IPv4 + IPv6

## Vision: After Phase 10

After all 10 phases, Heph4estus will be a **general-purpose distributed security tool platform** that looks like this:

### For the operator

```
$ heph scan --tool nuclei --file targets.txt --workers 100 --cloud hetzner
Deploying infrastructure (hetzner)...
  Controller VM: 10.0.1.5 (NATS + S3)
  Worker VMs: 100 x cx21 (Hetzner Cloud)
Enqueueing 2,847 targets...
Launching 100 workers...
Scanning... 1,204/2,847 (42.3%) — elapsed 3m22s — 100 unique source IPs
```

Or through the TUI:

```
┌─────────────────────────────────────┐
│         HEPH4ESTUS                  │
├─────────────────────────────────────┤
│  ► Nmap Scanner                     │
│    Nuclei                           │
│    Subfinder                        │
│    httpx                            │
│    ffuf                             │
│    Masscan                          │
│    Naabu + Nmap Pipeline            │
│    Gobuster                         │
│    ─────────────────                │
│    Settings                         │
└─────────────────────────────────────┘
```

The TUI main menu is dynamically populated from the module registry. `nmap` keeps its specialized config/status/results views (richer UX for nmap-specific options), but runs on the same generic worker/infrastructure as all other tools. Built-in generic modules use the shared generic config -> deploy -> scan -> results path. Wordlist-driven modules (`ffuf`, `gobuster`, `feroxbuster`) use the same generic runtime with chunked wordlist uploads.

### For the tool author

Adding a new tool to the platform requires:

1. A YAML module definition (5-10 lines)
2. A container build command (one line in the Makefile)

No Go code. No custom Terraform. No custom worker binary.

```yaml
# internal/modules/definitions/my-tool.yaml
name: my-tool
description: My custom security tool
command: "my-tool -i {{input}} -o {{output}}"
input_type: target_list
output_ext: json
install_cmd: "go install github.com/author/my-tool@latest"
default_cpu: 256
default_memory: 512
timeout: 10m
tags: [scanner, custom]
```

```makefile
docker-build-my-tool:
	docker build --build-arg TOOL_INSTALL_CMD="go install github.com/author/my-tool@latest" \
	  -t scanner-my-tool containers/generic/
```

### Platform capabilities

| Capability | Description |
|-----------|-------------|
| **14+ built-in tools** | nmap, nuclei, ffuf, subfinder, httpx, masscan, naabu, gobuster, feroxbuster, dnsx, katana, gospider, massdns, dalfox, gowitness |
| **Custom tools** | Any tool that reads an input file and writes an output file — add via YAML |
| **Multi-cloud** | AWS (Fargate + Spot Fleet), Hetzner, Linode, Scaleway, Vultr, any VPS with S3-compatible storage |
| **Workload distribution** | Target splitting (most tools), wordlist splitting (ffuf, gobuster), port splitting (nmap) |
| **Fault tolerance** | Automatic retry via message queue visibility timeout, dead-letter queue for poison pills, error classification (transient vs permanent) |
| **Scan hardening** | Pre-scan jitter, timing templates, DNS server passthrough, reverse DNS disable |
| **IP diversity** | Multi-NAT Gateway on AWS, IPv6 via EIGW, inherent per-VM IP diversity on VPS providers |
| **Result pipelines** | JSON/CSV/JSONL export, pipe-friendly CLI, tool-specific formatters |
| **Cost efficiency** | Ephemeral infrastructure (deploy on demand, destroy after), EC2 Spot for 60-90% savings, VPS providers for even cheaper scanning |
| **Two interfaces** | Interactive TUI for exploration, scriptable CLI for automation and CI/CD |

### Recon pipeline example

```bash
# Full recon pipeline: subdomain discovery -> HTTP probing -> port scanning -> vuln scanning
heph scan --tool subfinder --file domains.txt --format jsonl \
  | jq -r '.host' > subdomains.txt

heph scan --tool httpx --file subdomains.txt --format jsonl \
  | jq -r '.url' > live-hosts.txt

heph scan --tool nmap --file live-hosts.txt --workers 50 --no-rdns --format json \
  > scan-results.json

heph scan --tool nuclei --file live-hosts.txt --workers 200 --cloud hetzner \
  --format jsonl | jq 'select(.severity == "critical" or .severity == "high")'
```

### What Heph4estus is not

- **Not a vulnerability management platform** — it runs tools and collects results; it doesn't deduplicate, track, or remediate findings
- **Not a replacement for manual testing** — it automates the "run this tool against N targets" workflow, not the analysis
- **Not a SIEM or log aggregator** — results are stored in S3/object storage, not indexed for search
- **Not for sustained long-running infrastructure** — deploy, scan, destroy. The NAT Gateway alone costs $0.045/hr if left running

### Comparison to existing tools

| | Axiom/Ax | Heph4estus |
|---|---------|-----------|
| Architecture | SSH from laptop to VMs | Producer-consumer with managed services |
| Fault tolerance | None (work lost on VM death) | Automatic retry via SQS visibility timeout |
| Scaling | ~100 VMs (SSH bottleneck) | 1000+ workers (queue-based, no bottleneck) |
| Work distribution | Static pre-split | Dynamic pull (workers grab tasks as they finish) |
| Multi-cloud | 9 providers (SSH everywhere) | AWS + any VPS with S3-compatible storage |
| Tool support | 70+ (simple JSON definitions) | 14+ built-in, extensible via YAML |
| IaC | Packer images, bash scripts | Terraform modules |
| UI | CLI only | TUI + CLI |
| Result storage | Local only | Cloud object storage (downloadable) |
| Cost model | Hourly VM billing | Per-second (Fargate) or spot pricing |
