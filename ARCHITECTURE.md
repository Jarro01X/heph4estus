# Heph4estus Architecture

## Vision

Heph4estus is a TUI application that automates the deployment of red team tooling into cloud infrastructure using distributed systems. Users interact with a terminal UI to configure, launch, monitor, and collect results from cloud-scaled security tools.

**Core approach:** A generalized module system that supports any "input → run → output" security tool via YAML definitions and a generic worker. Custom infrastructure is only needed for tools with unique requirements (GPU compute, multi-phase pipelines).

**Phase 1 (AWS):** Nmap scanning, generalized module system for 60+ tools, naabu+nmap pipeline
**Future:** Hashcat (GPU cracking), GCP and Azure support

## Project Structure

```
heph4estus/
├── cmd/
│   ├── heph4estus/main.go             # TUI entry point
│   ├── heph/main.go              # CLI entry point (pipeline-friendly)
│   └── workers/
│       ├── nmap/main.go               # Nmap consumer (runs in ECS Fargate)
│       └── generic/main.go            # Generic worker (any module definition)
├── internal/
│   ├── tui/                           # Terminal UI layer
│   │   ├── app.go                     # Root Bubbletea model
│   │   ├── styles.go                  # Lipgloss styles
│   │   ├── keys.go                    # Key bindings
│   │   └── views/
│   │       ├── menu/model.go          # Main menu (dynamic from module registry)
│   │       ├── deploy/model.go        # Shared deploy view (terraform + docker)
│   │       ├── nmap/
│   │       │   ├── config.go          # Nmap job configuration view
│   │       │   └── status.go          # Nmap job monitoring view
│   │       └── generic/
│   │           ├── config.go          # Generic module configuration view
│   │           └── results.go         # Generic results view with tool formatters
│   ├── modules/                       # Generalized module system
│   │   ├── module.go                 # ModuleDefinition struct, validation
│   │   ├── registry.go              # Registry: load, list, lookup modules
│   │   └── definitions/             # Embedded YAML module definitions
│   │       ├── nmap.yaml
│   │       ├── nuclei.yaml
│   │       ├── subfinder.yaml
│   │       ├── httpx.yaml
│   │       ├── ffuf.yaml
│   │       ├── masscan.yaml
│   │       ├── gobuster.yaml
│   │       ├── feroxbuster.yaml
│   │       ├── dnsx.yaml
│   │       ├── katana.yaml
│   │       └── ...                   # Additional tool definitions
│   ├── infra/                         # Infrastructure lifecycle management
│   │   ├── terraform.go              # Init, plan, apply, destroy, read outputs
│   │   ├── docker.go                 # Build, tag, push
│   │   └── ecr.go                    # ECR authentication
│   ├── cloud/                         # Cloud provider abstraction
│   │   ├── provider.go               # Interfaces
│   │   ├── mock/                     # Mock provider for testing
│   │   └── aws/
│   │       ├── provider.go           # AWS implementation
│   │       ├── sqs.go
│   │       ├── s3.go
│   │       ├── sfn.go
│   │       ├── ecs.go
│   │       └── ec2.go                # EC2 Spot Fleet, NAT Gateway queries, EIGW status
│   ├── worker/                        # Shared worker utilities
│   │   ├── jitter.go                 # Jitter/pacing (used by all workers)
│   │   └── errors.go                 # Shared failure classification types
│   ├── tools/                         # Tool-specific logic (only for non-generic tools)
│   │   ├── nmap/
│   │   │   ├── scanner.go            # Nmap execution
│   │   │   ├── portparse.go          # Port parsing/splitting
│   │   │   ├── errors.go             # Error classification (permanent/transient/partial)
│   │   │   ├── jitter.go             # Scan jitter and timing templates
│   │   │   ├── dns.go                # DNS resolver distribution
│   │   │   └── models.go
│   │   ├── naabu/
│   │   │   ├── scanner.go            # Naabu execution
│   │   │   └── models.go
│   │   └── wordlist/
│   │       └── splitter.go           # Shared wordlist splitting (ffuf, gobuster, etc.)
│   ├── config/config.go
│   └── logger/logger.go
├── deployments/                       # Infrastructure as Code
│   └── aws/
│       ├── shared/                    # Shared Terraform modules
│       │   ├── networking/            # VPC, subnets, NAT, EIGW, multi-NAT
│       │   └── security/             # Base IAM roles
│       ├── nmap/                      # Nmap-specific infra (existing)
│       │   ├── environments/dev/
│       │   ├── compute/
│       │   ├── messaging/
│       │   ├── spot/                 # EC2 Spot Fleet IAM + AMI
│       │   └── storage/
│       └── generic/                   # Generic module infra (parameterized)
│           ├── environments/dev/
│           ├── compute/              # ECS cluster, Fargate task (cpu/memory vars)
│           ├── messaging/            # SQS queue + DLQ
│           └── storage/              # S3 bucket
├── containers/
│   ├── nmap/Dockerfile                # Existing nmap container
│   ├── generic/Dockerfile             # Generic container with TOOL_INSTALL_CMD arg
│   └── naabu/Dockerfile               # Naabu container (if not using generic)
├── ARCHITECTURE.md
├── WORKLOAD_DISTRIBUTION.md
└── go.mod
```

## User Experience

**The user only needs to:**
1. Authenticate with their cloud provider (e.g., `aws sso login`, set API keys)
2. Provide input files (targets.txt, hashes.txt)

**Heph4estus handles everything else:** infrastructure provisioning, container image building/pushing, job submission, monitoring, result collection, and teardown. The user never runs `terraform`, `docker`, or `aws` commands directly.

## CLI Support

Heph4estus provides both a TUI and a CLI entry point. Both call the same underlying tool and cloud logic — the UI layer is just a thin wrapper.

```
cmd/heph4estus/main.go   → TUI (Bubbletea, interactive)
cmd/heph/main.go     → CLI (flags + stdin/stdout, pipeline-friendly; replaces current cmd/producer)
```

### CLI Usage

```bash
# Nmap scan (handles full lifecycle: deploy → scan → collect results)
heph nmap --file targets.txt --mode target-ports --port-chunks 5

# Nmap scan with networking options
heph nmap --file targets.txt --enable-ipv6 --multi-nat

# Naabu + Nmap pipeline
heph naabu-nmap --file targets.txt --nmap-options "-sV"

# ffuf web fuzzing (wordlist splitting)
heph ffuf --url https://target.com/FUZZ --wordlist /path/to/wordlist.txt --chunks 10
heph ffuf --url https://target.com/FUZZ --wordlist big.txt --chunks 50 --options "-mc 200,301"

# Hashcat crack (mask attack)
heph hashcat --hashes hashes.txt --attack-mode 3 --mask '?a?a?a?a' --instances 5

# Hashcat crack (wordlist + rules, distributed across 10 spot instances)
heph hashcat --hashes hashes.txt --attack-mode 0 --wordlist rockyou.txt \
    --rules best64.rule --instances 10

# Hashcat crack (wordlist + stacked rules)
heph hashcat --hashes hashes.txt --attack-mode 0 --wordlist rockyou.txt \
    --rules best64.rule --rules toggles.rule --instances 20

# Infrastructure only (shows plan, prompts for approval)
heph infra deploy --tool nmap
heph infra deploy --tool nmap --auto-approve
heph infra destroy --tool nmap

# Check job status (JSON output for piping)
heph status --job-id abc123 --format json
```

### Design Principle

The `internal/tools/` and `internal/cloud/` packages contain all logic. Both `cmd/heph4estus` (TUI) and `cmd/heph` (CLI) are thin entry points that:
1. Parse user input (interactive forms vs. flags)
2. Call the same tool/cloud functions
3. Present output (rendered views vs. stdout)

This means any feature available in the TUI is also available via CLI, enabling automated pipelines, CI/CD integration, and scripting.

## Infrastructure Lifecycle

All infrastructure management is handled internally by the TUI/CLI. The user never interacts with Terraform or Docker directly.

### Deploy Flow

```
User submits job config
      │
      ▼
  Terraform Init
      │
      ▼
  Terraform Plan ──► Show plan summary to user
      │
      ▼
  User approves ──► Terraform Apply (streamed to viewport/stdout)
      │
      ▼
  Build container image
      │
      ▼
  Push to ECR
      │
      ▼
  Batch-enqueue targets to SQS
      │
      ▼
  Launch N Fargate workers (worker pool)
      │
      ▼
  Monitor progress (S3 count) → Collect results
      │
      ▼
  User triggers cleanup ──► Terraform Destroy
```

### What the TUI/CLI manages internally
- `terraform init`, `plan`, `apply`, `destroy` — shelled out with correct working directories
- `docker build`, `docker tag`, `docker push` — container image lifecycle
- ECR authentication (`aws ecr get-login-password`)
- Reading Terraform outputs (ARNs, URLs) to configure jobs
- S3 bucket cleanup before destroy

### What the user provides
- Cloud provider authentication (AWS SSO, env vars, or credentials file)
- Input files (target lists, hash files, wordlists)

## TUI Architecture (Bubbletea)

### Model Hierarchy

```
App (root model)
├── MainMenu (dynamically populated from module registry)
│   ├── "Nmap Scanner" → NmapView
│   ├── "Naabu + Nmap" → NaabuNmapView
│   ├── "<tool>" → GenericView (for each registered module)
│   └── "Settings" → SettingsView
├── NmapView
│   ├── ConfigForm (target file, mode, options, port-chunks, networking toggles)
│   ├── DeployView (plan → approve → apply → image push)
│   ├── StatusView (live job monitoring)
│   └── ResultsView (S3 result browser)
├── NaabuNmapView
│   ├── ConfigForm (target file, nmap options for phase 2)
│   ├── DeployView (plan → approve → apply → image push)
│   ├── StatusView (phase 1 progress → phase 2 progress)
│   └── ResultsView (combined scan results)
└── GenericView (shared across all generic module tools)
    ├── ConfigForm (input file, options — fields driven by module definition)
    ├── DeployView (plan → approve → apply → image push)
    ├── StatusView (per-task progress)
    └── ResultsView (tool-specific formatters for known output formats)
```

### Navigation Flow

```
┌──────────────────────┐
│      Main Menu       │
│  ┌────────────────┐  │
│  │ Nmap Scanner   │──┼──► Config ──► Plan ──► Approve ──► Deploy ──► Status ──► Results
│  ├────────────────┤  │
│  │ Naabu + Nmap   │──┼──► Config ──► Plan ──► Approve ──► Deploy ──► Status (2 phases) ──► Results
│  ├────────────────┤  │
│  │ <module tools> │──┼──► Config ──► Plan ──► Approve ──► Deploy ──► Status ──► Results
│  ├────────────────┤  │   (nuclei, ffuf, subfinder, httpx, masscan, etc. — from module registry)
│  │ Settings       │──┼──► AWS credentials, region, defaults (Phase 2)
│  └────────────────┘  │
│  [q] quit            │
└──────────────────────┘
```

### Bubbletea Pattern

The root model (`internal/tui/app.go`) holds the active view and delegates `Update` and `View` calls:

```go
type App struct {
    activeView View
    provider   cloud.Provider
    width      int
    height     int
}

type View interface {
    Init() tea.Cmd
    Update(tea.Msg) (View, tea.Cmd)
    View() string
}
```

Each view (menu, nmap config, hashcat status, etc.) implements the `View` interface. Navigation happens by swapping `activeView` in the root model.

### Key Dependencies
- `github.com/charmbracelet/bubbletea` — core TUI framework
- `github.com/charmbracelet/bubbles` — text inputs, spinners, tables, progress bars, viewports
- `github.com/charmbracelet/lipgloss` — styling, layout, borders, colors
- `github.com/charmbracelet/huh` — form builder with validation, styled inputs, selects, confirms

## TUI Design

### Color Palette (Forge Theme)

| Name | Hex | Usage |
|---|---|---|
| **Ember** | `#C75B39` | Active selections, highlights, progress bar fills |
| **Forge Gold** | `#D4A04A` | Titles, accent text, success states |
| **Molten** | `#E8873A` | Spinners, animated elements, loading states |
| **Iron** | `#43464B` | Primary text |
| **Steel** | `#6E7C7F` | Borders, secondary text, muted elements |
| **Charcoal** | `#2B3338` | Panel backgrounds, inactive areas |
| **Slag** | `#5C2E1A` | Errors, destructive actions |
| **White Hot** | `#F5E6C8` | Critical emphasis, completed states |

These are defined as lipgloss adaptive colors in `internal/tui/styles.go`.

### Main Menu (Centered Full-Screen)

The main menu is a full-screen splash with the anvil art and title centered:

```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                      │
│                                  [anvil ASCII art]                                   │
│                              ~8-10 lines, ~40-50 chars                               │
│                            rendered in Ember / Forge Gold                             │
│                                                                                      │
│  ██╗  ██╗███████╗██████╗ ██╗  ██╗██╗  ██╗███████╗███████╗████████╗██╗   ██╗███████╗  │
│  ██║  ██║██╔════╝██╔══██╗██║  ██║██║  ██║██╔════╝██╔════╝╚══██╔══╝██║   ██║██╔════╝  │
│  ███████║█████╗  ██████╔╝███████║███████║█████╗  ███████╗   ██║   ██║   ██║███████╗  │
│  ██╔══██║██╔══╝  ██╔═══╝ ██╔══██║╚════██║██╔══╝  ╚════██║   ██║   ██║   ██║╚════██║  │
│  ██║  ██║███████╗██║     ██║  ██║     ██║███████╗███████║   ██║   ╚██████╔╝███████║  │
│  ╚═╝  ╚═╝╚══════╝╚═╝     ╚═╝  ╚═╝     ╚═╝╚══════╝╚══════╝   ╚═╝    ╚═════╝ ╚══════╝  │
│                    Forge Gold ──────────────────┤ 4 in Ember                          │
│                                                                                      │
│                           ► Nmap Scanner                                             │
│                             Naabu + Nmap                                             │
│                             Nuclei / ffuf / ...  (from module registry)              │
│                             Settings                                                 │
│                                                                                      │
├──────────────────────────────────────────────────────────────────────────────────────┤
│  [↑↓] Navigate  [enter] Select  [q] Quit                                            │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

- Title text rendered in **Forge Gold** with the `4` in **Ember** to make it pop
- Menu uses `bubbles/list` with a custom item delegate styled in the forge palette
- Status bar uses `bubbles/help` with **Steel** text on **Charcoal** background
- Anvil art to be designed during implementation (~8-10 lines tall, ~40-50 chars wide)

### Inner View Layout

Once a tool is selected, views (config, deploy, status, results) use the full terminal width with no anvil art:

```
┌─── Nmap Scanner ─── Config ─────────────────────────────────────────────────────────┐
│                                                                                      │
│  [full-width content area]                                                           │
│                                                                                      │
│  Config forms (Huh), deploy output (viewport),                                       │
│  status tables/progress bars, or result tables                                       │
│                                                                                      │
├──────────────────────────────────────────────────────────────────────────────────────┤
│  [tab] Next field  [enter] Submit  [esc] Back                                        │
└──────────────────────────────────────────────────────────────────────────────────────┘
```

- Compact one-line title bar in **Forge Gold** showing tool name and current step
- Full-width content area for forms, output, tables
- Status bar with context-appropriate key bindings

### Component Mapping

| View | Components | Notes |
|---|---|---|
| Main menu | `bubbles/list` | Custom item delegate with forge-styled highlight |
| Config forms | `huh` | Inputs, selects, confirms with validation |
| Plan review | `bubbles/viewport` | Terraform plan output rendered in scrollable viewport, approve/reject prompt |
| Deploy progress | `bubbles/spinner` + `bubbles/viewport` | Multi-stage: terraform apply → docker build → ECR push. Streamed output in viewport |
| Job status | `bubbles/progress` + `bubbles/table` | Per-task progress bars in Ember, task table with status column |
| Results | `bubbles/table` + `bubbles/viewport` | Table for overview, viewport for detail drill-down |
| Cleanup | `huh` confirm + `bubbles/spinner` | Confirm destroy, then terraform destroy with streamed output |
| Status bar | `bubbles/help` | Steel text on Charcoal background, context-aware key bindings |

### Styling Conventions (`internal/tui/styles.go`)

- **Borders:** `lipgloss.RoundedBorder()` in **Steel** for all panels
- **Active border:** **Ember** when a panel has focus
- **Title bar:** **Forge Gold** bold text, left-aligned
- **Selected item:** **Ember** foreground with bold
- **Muted text:** **Steel** for labels, hints, inactive items
- **Error text:** **Slag** bold
- **Success text:** **Forge Gold**

## Nmap Architecture

*(Enhanced with workload distribution — see WORKLOAD_DISTRIBUTION.md)*

### Data Flow

```
TUI/CLI (Nmap Config)
      │
      │ User configures: targets, options, worker count
      ▼
  Batch-enqueue targets to SQS (10/batch)
      │
      ▼
  Launch N workers via ECS RunTask
  (Fargate or EC2 Spot — auto-selected by ComputeMode)
      │
  ┌───┼───┐
  ▼   ▼   ▼
 Workers poll SQS (long-running loop)
  │   │   │
  ▼   ▼   ▼
 nmap executions
  │   │   │
  ▼   ▼   ▼
 Upload results to S3
      │
      ▼
 TUI polls S3 object count for progress
      │
      ▼
 TUI/CLI (Results View) — paginated, on-demand download
```

**Worker Pool Model:** The TUI/CLI batch-sends all targets to SQS, then launches N
long-running workers (Fargate or EC2 Spot) that each loop: poll SQS → run nmap →
upload results to S3 → repeat until queue is empty. Workers exit when the queue is
empty — no orchestrator is needed. Progress is tracked via S3 object count vs total
targets. This scales to hundreds of thousands of targets with configurable worker
concurrency.

**Automatic retry on crash:** SQS visibility timeout provides automatic retry. If a
worker crashes or is terminated (e.g., spot reclamation), unacknowledged messages
become visible again after the timeout expires and are picked up by other workers.
No work is lost.

### IAM Roles

| Role | Purpose |
|---|---|
| **ECS Execution Role** | Allows ECS to pull container images from ECR and write CloudWatch logs |
| **ECS Task Role** | Grants the running container access to SQS (poll/delete) and S3 (upload) |
| **EC2 Instance Profile** | (Spot only) SQS poll/delete, S3 upload, ECR pull, EC2 self-terminate |

### S3 Result Keys
```
nmap/{target}_{timestamp}.json                                    # target-only mode
nmap/{group_id}/{target}_chunk{N}_of_{total}_{timestamp}.json     # target-ports mode
```

## Naabu + Nmap Pipeline

### Overview

Naabu is a fast port scanner from ProjectDiscovery, written in Go. It supports SYN/CONNECT/UDP probes, IPv4/IPv6, and has a built-in `-nmap-cli` flag that pipes discovered open ports directly into nmap within the same process. This eliminates all multi-phase orchestration — a single worker runs `naabu -host <target> -nmap-cli 'nmap <flags>'` in one pass.

This is critical for Jason Haddix's IPv6 scanning use case at massive scale: fast port discovery + deep service detection in a single worker, using the same worker pool model as everything else.

### Data Flow

```
TUI/CLI (Naabu+Nmap Config)
      │
      │ User provides: targets, nmap options, mode (combined/discovery-only)
      ▼
  SQS Queue (batch enqueue targets)
      │
  ┌───┼───┐
  ▼   ▼   ▼
 N Workers (poll loop)
  │   │   │
  ▼   ▼   ▼
 Each worker runs:
   naabu -host {target} -nmap-cli 'nmap {options} -oX {output}'
   (naabu discovers ports → pipes directly to nmap → one pass)
  │   │   │
  ▼   ▼   ▼
 S3 results ──► TUI/CLI (Results View)
```

### The `-nmap-cli` Flag

Naabu's `-nmap-cli` flag runs nmap inline after port discovery completes. The flow within a single worker process:

1. Naabu sends SYN probes to all 65,535 ports (fast, lightweight)
2. Naabu collects open ports (typically ~5-20 per target)
3. Naabu invokes nmap only against discovered open ports with the user's flags
4. Worker uploads the nmap XML output to S3

No Lambda, no second queue, no bridging logic. One worker, one pass.

### Two Modes

| Mode | Command | Output | Use when |
|---|---|---|---|
| **Combined** (default) | `naabu -host {target} -nmap-cli 'nmap -sV -oX {output}'` | Nmap XML | You want port discovery + deep scan in one shot |
| **Discovery-only** | `naabu -host {target} -json -o {output}` | Naabu JSON | You only need open port lists |

### IPv6 Support

Naabu supports IPv6 natively. For IPv6 targets, the combined command becomes:

```
naabu -host 2001:db8::1 -nmap-cli 'nmap -6 -sV -oX {output}'
```

### Why Single-Phase

| Approach | Ports scanned | Orchestration | Time |
|---|---|---|---|
| `nmap -sV -p-` directly | 65,535 ports, deep scan each | Simple | Very slow |
| Two-phase (naabu → Lambda → nmap) | 65,535 fast + ~5 deep | Complex (Lambda, 2 queues, SFN) | Fast |
| `-nmap-cli` combined | 65,535 fast + ~5 deep | Simple (same as nmap) | Fast |

The combined approach gets the same speed benefit as two phases but with zero orchestration complexity. Same worker pool model as nmap — just a different command.

### S3 Result Keys

```
naabu-nmap/{target}_{timestamp}.xml        # Combined mode: nmap XML output
naabu/{target}_{timestamp}.json            # Discovery-only mode: naabu JSON
```

### Container Image

Naabu+nmap worker (`containers/naabu/Dockerfile`):
```
Base: golang:<version>-alpine (build) → alpine:3.19 (runtime)
Install: naabu binary + nmap (both needed for combined mode)
User: non-root
```

Naabu does not require raw socket access for CONNECT scans, making it compatible with Fargate. For SYN scans, it needs `NET_RAW` capability (same as nmap).

## Scan Hardening

Hardens nmap workers before layering on networking complexity or adding new tools. Addresses feedback that scans are currently loud and fragile.

### Retry Logic and Failure Classification

Workers classify scan outcomes before deciding what to do with the SQS message:

| Classification | Action | Examples |
|---|---|---|
| **Success** | Delete SQS message, upload results | Clean scan, all hosts responded |
| **Permanent failure** | Delete SQS message, log error | Invalid target, permission denied, malformed options |
| **Transient failure** | Let message return to queue (visibility timeout) | DNS timeout, host unreachable, network flap |
| **Partial success** | Delete message, upload results with warnings | Some hosts scanned, some failed |

This prevents infinite retries on bad input while recovering from temporary network issues.

### Jitter and Pacing

Workers apply random jitter before starting scans to prevent all containers from hitting targets simultaneously:

- `JITTER_MAX_SECONDS` env var controls max random delay (default: 0 = no jitter)
- `NMAP_TIMING_TEMPLATE` env var injects nmap's `-T` flag (T0=paranoid through T5=insane)
- Jitter uses `crypto/rand` for unpredictable delays

### DNS Options

Two CLI flags control DNS behavior during scans:

- **`--dns-servers`** — Optional passthrough to nmap's `--dns-servers` flag. Users specify resolvers explicitly when needed (e.g., internal DNS for private engagements, or specific public resolvers). No automatic distribution — the same resolvers are passed to all workers.
- **`--no-rdns`** — Disables reverse DNS resolution (injects `-n` into nmap). This is the single biggest performance win for IP-based scans and reduces DNS fingerprinting. Reverse DNS is the primary source of DNS load at scale.

Default behavior: no DNS modification. Nmap uses the container's default resolver (VPC DNS).

## ffuf Architecture

### Overview

Distributed web fuzzing using ECS Fargate. Follows the **wordlist splitting** pattern — a wordlist is split into N chunks, each container fuzzes its chunk against the target URL. Same producer-consumer pattern as nmap, no GPU or spot instances needed.

### Data Flow

```
TUI/CLI (ffuf Config)
      │
      │ User provides: target URL (with FUZZ keyword), wordlist, chunk count, options
      ▼
  Producer Logic
      │
      │ 1. Count wordlist lines
      │ 2. Split wordlist into N chunks
      │ 3. Upload chunks to S3
      │ 4. Send each chunk as SQS message
      ▼
  Worker pool: N Fargate workers poll SQS
      │
  ┌───┼───┐
  ▼   ▼   ▼
 Workers run ffuf (JSON output mode)
  │   │   │
  ▼   ▼   ▼
 S3 results ──► TUI/CLI polls S3 count for progress
```

### S3 Key Structure (ffuf)

```
ffuf/jobs/{job_id}/
├── input/
│   ├── wordlist_chunk_0.txt
│   ├── wordlist_chunk_1.txt
│   └── ...
├── results/
│   ├── chunk_0_results.json
│   ├── chunk_1_results.json
│   └── ...
└── status.json
```

### Wordlist Splitting

Splitting is streaming and memory-safe — designed to handle TB-scale wordlists without loading them into memory. Each chunk is written to a separate file and uploaded to S3.

```
wordlist.txt (10M lines)
      │
  SplitWordlist(path, 10, outputDir)
      │
  ┌───┼───┬───┬───┬───┬───┬───┬───┬───┐
  ▼   ▼   ▼   ▼   ▼   ▼   ▼   ▼   ▼   ▼
chunk_0 chunk_1 ... chunk_9  (1M lines each)
```

Chunk sizing affects per-container memory — ffuf loads the wordlist chunk into memory. The default Fargate task memory is 512 MB (configurable via `task_memory` Terraform variable). At ~20 bytes/line, a 1M-line chunk uses ~20 MB (comfortable), but 10M-line chunks push ~200 MB before Go runtime and response buffering overhead. Prefer increasing chunk count over increasing task memory.

### Container Image

ffuf worker (`containers/ffuf/Dockerfile`):
```
Base: alpine:3.19
Install: ffuf binary (Go, downloaded from releases)
User: non-root
```

ffuf is a single Go binary with no external dependencies, making the container image small and simple.

## Generalized Module System

### Overview

~90% of axiom's 86+ tools follow the same "input file → run command → output file" pattern (see `COMPARISON.md`). Instead of building custom Go workers and Terraform for each tool, Heph4estus defines tools as simple module definitions and uses a generic worker binary + generic Terraform module.

### Module Definition Format

Tools are defined as YAML files embedded in the binary via `//go:embed`:

```yaml
# internal/modules/definitions/nuclei.yaml
name: nuclei
description: Template-based vulnerability scanner
command: "nuclei -l {{input}} -o {{output}} -j"
input_type: target_list
output_ext: jsonl
install_cmd: "go install github.com/projectdiscovery/nuclei/v3/cmd/nuclei@latest"
default_cpu: 256
default_memory: 512
timeout: 10m
tags: [scanner, vuln]
```

The Go struct (`internal/modules/module.go`):

```go
type ModuleDefinition struct {
    Name          string            `yaml:"name"`
    Description   string            `yaml:"description"`
    Command       string            `yaml:"command"`       // {{input}}, {{output}}, {{target}}, {{wordlist}}
    InputType     string            `yaml:"input_type"`    // "target_list" or "wordlist"
    OutputExt     string            `yaml:"output_ext"`
    InstallCmd    string            `yaml:"install_cmd"`   // for generic Dockerfile
    DefaultCPU    int               `yaml:"default_cpu"`   // Fargate CPU units
    DefaultMemory int               `yaml:"default_memory"` // Fargate memory in MB
    Timeout       string            `yaml:"timeout"`
    Tags          []string          `yaml:"tags"`
    Env           map[string]string `yaml:"env,omitempty"`
}
```

### Built-in Module Definitions

| Module | Input Type | Pattern | Description |
|--------|-----------|---------|-------------|
| nmap | target_list | target splitting | Network port scanner |
| nuclei | target_list | target splitting | Template-based vulnerability scanner |
| subfinder | target_list | target splitting | Subdomain discovery |
| httpx | target_list | target splitting | HTTP probing |
| masscan | target_list | target splitting | Ultra-fast port scanner |
| dnsx | target_list | target splitting | DNS toolkit |
| katana | target_list | target splitting | Web crawler |
| gospider | target_list | target splitting | Web spider |
| ffuf | wordlist | wordlist splitting | Web fuzzer |
| gobuster | wordlist | wordlist splitting | Directory/DNS brute forcer |
| feroxbuster | wordlist | wordlist splitting | Recursive content discovery |

### Generic Worker

A single worker binary (`cmd/workers/generic/main.go`) handles all module definitions:

```
1. Read TOOL_NAME env var → load module definition from embedded registry
2. Pull task from queue (via cloud.Queue interface)
3. Download input file(s) from storage (via cloud.Storage interface)
4. Template command: substitute {{input}}, {{output}}, {{target}}, {{wordlist}}
5. Execute command with configured timeout
6. Classify result (success / transient failure / permanent failure)
7. Upload output to storage on success
8. Acknowledge or leave message based on classification
```

### Generic Container Image

A single Dockerfile (`containers/generic/Dockerfile`) with a `TOOL_INSTALL_CMD` build arg:

```dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o /worker cmd/workers/generic/main.go

FROM alpine:3.19
ARG TOOL_INSTALL_CMD
RUN ${TOOL_INSTALL_CMD}
COPY --from=builder /worker /usr/local/bin/worker
RUN adduser -D -s /bin/sh scanner
USER scanner
ENTRYPOINT ["/usr/local/bin/worker"]
```

Build per-tool images: `docker build --build-arg TOOL_INSTALL_CMD="apk add nmap" -t scanner-nmap .`

### Generic Terraform Module

A parameterized Terraform module (`deployments/aws/generic/`) replaces per-tool Terraform for standard tools:

```
deployments/aws/generic/
├── compute/         # ECS cluster, Fargate task (cpu/memory from variables)
├── messaging/       # SQS queue + DLQ
├── storage/         # S3 bucket
└── environments/dev/
    ├── main.tf      # tool_name, container_image, cpu, memory, concurrency
    ├── variables.tf
    └── outputs.tf
```

Variables: `tool_name`, `container_image`, `cpu`, `memory`, `max_concurrency`. Reuses shared networking and security modules.

### What Still Needs Custom Infrastructure

| Tool/Feature | Why Custom |
|--------------|-----------|
| **Nmap** (port splitting) | Custom producer logic for port parsing and splitting |
| **Hashcat** (future) | GPU compute, spot interruption handling, session resumption, keyspace splitting |

Everything else — nuclei, subfinder, httpx, masscan, ffuf, gobuster, feroxbuster, katana, gospider, massdns, dnsx, gowitness, dalfox, and dozens more — fits the generic module.

---

## Scaling Architecture (1M+ Targets)

The worker pool model (queue + N workers + object store) is inherently scalable — the queue is the load balancer. But at extreme scale (500k–5M+ targets), specific bottlenecks emerge at the edges. This section documents the scaling profile and planned mitigations.

### Scaling Profile

| Scale | Bottleneck | Mitigation | Status |
|---|---|---|---|
| <100k | None | Current design works | Done |
| 100k–500k | Tail latency from uneven targets | Target decomposition (expand CIDRs, split port ranges) | Phase 4 |
| 500k–1M | S3 progress counting O(n/1000), enqueue speed | Atomic `ProgressCounter`, parallel enqueue goroutines | Interface added |
| 1M+ | Fargate task limits (500–5000/account), cost | EC2 spot instances via `Compute.RunSpotInstances` | Done |
| 5M+ | Source IP blocking, NAT throughput | Multi-NAT subnets, IP rotation across workers | Phase 10 |

### Bottleneck 1: Heterogeneous Scan Times

Not all targets are equal work. `192.168.1.1 -sS` takes 2 seconds; `10.0.0.0/24 -sV -A -p-` takes 45 minutes. With a naive queue, 199 workers finish quickly while 1 worker is stuck on a /16 for hours.

**Solution: Target decomposition before enqueueing.** The TUI/CLI should:
- Expand CIDR ranges into individual IPs
- Split large port ranges across multiple SQS messages (port chunking)
- Result: every message is roughly equal work (~2 min scan)

This is already planned for Phase 4 (port splitting). With uniform work units, the simple queue model self-balances perfectly.

### Bottleneck 2: Progress Tracking at Scale

The current approach uses `Storage.Count()` (S3 `ListObjectsV2`, 1000 keys/page) to check how many results exist. At 1M results, that's 1000 API calls per progress poll — wasteful and slow.

**Solution: `ProgressCounter` interface.** Workers atomically increment a counter after each successful upload. The TUI reads the counter in O(1) instead of O(n/1000).

```go
// cloud.ProgressCounter — O(1) progress tracking for large-scale jobs.
type ProgressCounter interface {
    Increment(ctx context.Context, counterID string) error
    Get(ctx context.Context, counterID string) (int, error)
}
```

Implementations:
- **AWS:** DynamoDB atomic counter (`UpdateItem` with `ADD`)
- **Hetzner/Linode:** Redis `INCR` or NATS KV
- **Fallback:** `Storage.Count()` (works at <100k, just slower)

The status view's `ProgressTracker` uses `ProgressCounter.Get()` when available, falling back to `Storage.Count()`.

### Bottleneck 3: Enqueue Speed

`SendBatch` at 10 messages/API call = 100k API calls for 1M targets. At ~1ms/call ≈ 100s. Acceptable, but at 5M+ it becomes the dominant wait.

**Solution: Parallel enqueue.** Shard the target list into N chunks, enqueue from N goroutines. SQS/NATS handle concurrent producers. No architecture change needed — just an optimization inside `JobSubmitter.EnqueueTargets`.

### Bottleneck 4: Compute Limits

Fargate: 500–5000 concurrent tasks per account (requires limit increase). At 1M targets with 200 workers at 2min/target = 167 hours. Need more workers or faster workers.

**Solution: Spot instances.** The `cloud.Compute` interface already has `RunSpotInstances`. For 1M+ targets:
- Launch 500+ EC2 spot instances at ~$0.01/hr (vs Fargate at ~$0.04/hr)
- Each runs the same Docker container
- If spot is reclaimed, SQS visibility timeout handles retry — no lost work
- 4x cheaper than Fargate at scale

### Multi-Cloud Provider Architecture

The worker pool model simplifies multi-cloud because workers self-coordinate via the queue. No external orchestrator (SFN, Temporal) is needed.

| Concern | AWS | Hetzner / Linode / VPS |
|---|---|---|
| Queue | SQS | NATS JetStream |
| Storage | S3 | Provider S3-compatible storage or MinIO |
| Progress | DynamoDB counter | Redis INCR or NATS KV |
| Workers | Fargate or EC2 spot | Docker on VPS (via SSH or Nomad) |
| Provisioning | Terraform (AWS provider) | Terraform (Hetzner/Linode provider) |

**Why NATS JetStream:** Single binary, trivial to deploy, built-in persistence, ack-wait (visibility timeout equivalent), max-deliver (DLQ equivalent), consumer groups. Runs on a small VPS alongside the worker pool.

**Why not Temporal/orchestrators:** The worker pool model makes external orchestrators unnecessary. Workers are the orchestrator — they poll, process, upload, repeat. The queue handles coordination, retries, and load balancing. Adding Temporal would add operational complexity with no scaling benefit.

A `cloud.Provider` for Hetzner would look like:

```
HetznerProvider
├── NATSQueue       → implements cloud.Queue
├── S3Storage       → implements cloud.Storage (Hetzner S3-compat endpoint)
├── RedisCounter    → implements cloud.ProgressCounter
└── SSHCompute      → implements cloud.Compute (docker run over SSH)
```

---

## EC2 Spot Fleet (Auto-Select)

For large worker counts (>=50), heph4estus auto-selects EC2 Spot Fleet over Fargate. This is controlled by the `ComputeMode` field on `DeployConfig`:

| ComputeMode | Behavior |
|---|---|
| `"auto"` (default) | Uses Fargate for <50 workers, EC2 Spot Fleet for >=50 |
| `"fargate"` | Always uses ECS Fargate |
| `"spot"` | Always uses EC2 Spot Fleet |

### CompositeCompute Pattern

The `CompositeCompute` struct delegates to the appropriate backend:
- `RunContainer` → `ECSClient` (Fargate tasks)
- `RunSpotInstances` → `EC2Client` (Spot Fleet)

### EC2Client.RunSpotInstances

Creates an ephemeral launch template and calls the `CreateFleet` API with:
- **Allocation strategy:** `capacity-optimized-prioritized`
- **Fleet type:** `instant` (synchronous, returns instance IDs immediately)
- **Default instance types:** `c5.xlarge`, `c5a.xlarge`, `m5.xlarge`, `m5a.xlarge`

### UserData Generator

Each spot instance boots with a generated `UserData` script that:
1. Installs Docker
2. Authenticates to ECR (`aws ecr get-login-password`)
3. Pulls the worker container image
4. Runs the container with SQS/S3 environment variables
5. Self-terminates the instance when the container exits

### Terraform Spot Module

The spot module (`deployments/aws/generic/spot/`) provisions:
- **IAM instance profile** with permissions for SQS (poll/delete), S3 (upload), ECR (pull), and EC2 self-terminate (`ec2:TerminateInstances` scoped to own instance)
- **AL2023 AMI data source** for the launch template base image

---

## Scaling

### ProgressCounter Interface

For O(1) progress tracking at scale (vs `Storage.Count()` / S3 `ListObjectsV2` which is O(n/1000)):

```go
type ProgressCounter interface {
    Increment(ctx context.Context, counterID string) error
    Get(ctx context.Context, counterID string) (int, error)
}
```

### Planned Implementations

| Backend | Method | Use Case |
|---|---|---|
| DynamoDB | Atomic counter (`UpdateItem` with `ADD`) | AWS deployments |
| Redis | `INCR` command | Self-hosted / Hetzner / Linode |
| NATS KV | Key-value store with atomic operations | Lightweight self-hosted |

### Fallback Behavior

When no counter backend is configured, the system falls back to `Storage.Count()` (S3 `ListObjectsV2`). This works fine for jobs under ~100k targets but becomes slow at larger scale.

The TUI's `realTracker` in the status view will auto-select `ProgressCounter.Get()` when a counter backend is available, falling back to `Storage.Count()` otherwise. This auto-threshold is not yet implemented — currently the status view always uses S3 count.

---

## Future: Hashcat Architecture

### Overview

Distributed hash cracking using EC2 spot instances with GPUs. The key innovation is a **self-resumption loop**: when AWS reclaims a spot instance, the hashcat session is saved and the remaining work is re-queued automatically.

### Data Flow

```
TUI (Hashcat Config)
      │
      │ User provides: hash file, attack mode, wordlist/mask, rule files (optional), instance count
      ▼
  Producer Logic
      │
      │ 1. Upload hash file + wordlist + rule files to S3
      │ 2. Calculate keyspace including rules (hashcat --keyspace [-r rules...])
      │ 3. Split keyspace into N chunks
      │ 4. Send each chunk as SQS message (with rule_files_s3 references)
      ▼
  SQS Queue ◄──────────────────────────────────────┐
      │                                              │
  ┌───┼───┐                                          │
  ▼   ▼   ▼                                          │
 EC2 Spot Instances (GPU, e.g. g4dn.xlarge)          │
 ┌─────────────────────────────────┐                 │
 │  ┌───────────┐  ┌────────────┐  │                 │
 │  │  Hashcat   │  │  Sidecar   │  │                 │
 │  │  Worker    │  │  Monitor   │  │                 │
 │  │           ◄├──┤  (SIGUSR1) │  │                 │
 │  │  --skip X  │  │            │  │                 │
 │  │  --limit Y │  │ polls EC2  │  │                 │
 │  │            │  │ metadata   │  │                 │
 │  └─────┬──┬──┘  └──────┬─────┘  │                 │
 │        │  │             │         │                 │
 │        │  └─── on termination ───►│ save session    │
 │        │        notice:           │ upload to S3    │
 │        │        1. SIGUSR1        │ re-queue to ────┘
 │        │        2. wait for       │ SQS with
 │        │           checkpoint     │ remaining
 │        │        3. upload .restore│ keyspace
 │        │        4. re-queue       │
 │        │                          │
 └────────┼──────────────────────────┘
          │
          ▼  (on success or completion)
    S3 Results ──► TUI (Results View)
```

### Keyspace Splitting

Hashcat's keyspace is the total number of password candidates for a given attack. It can be queried:

```bash
# Mask attack
hashcat --keyspace -a 3 -m 0 '?a?a?a?a?a?a'
# Output: 735091890625

# Wordlist attack (no rules)
hashcat --keyspace -a 0 -m 0 wordlist.txt
# Output: 14344391

# Wordlist + rules — keyspace = wordlist × rule_count
hashcat --keyspace -a 0 -m 0 -r best64.rule wordlist.txt
# Output: 918361024  (14344391 × 64)

# Stacked rules — keyspace = wordlist × rules1 × rules2
hashcat --keyspace -a 0 -m 0 -r best64.rule -r toggles.rule wordlist.txt
# Output: (14344391 × 64 × toggle_count)
```

When rules are applied, the keyspace is the wordlist size multiplied by the total rule combinations. The `--skip` and `--limit` flags operate on this combined keyspace, so workload splitting works identically whether rules are present or not. See `WORKLOAD_DISTRIBUTION.md` for the full breakdown.

The producer splits the keyspace into chunks using `--skip` and `--limit`:

```
Total keyspace: 1,000,000
Chunks: 5

Chunk 1: --skip 0       --limit 200000
Chunk 2: --skip 200000  --limit 200000
Chunk 3: --skip 400000  --limit 200000
Chunk 4: --skip 600000  --limit 200000
Chunk 5: --skip 800000  --limit 200000
```

### SQS Message Format (Hashcat)

```json
{
  "job_id": "crack-2024-abc123",
  "hash_file_s3": "hashcat/jobs/crack-2024-abc123/hashes.txt",
  "wordlist_s3": "hashcat/jobs/crack-2024-abc123/wordlist.txt",
  "rule_files_s3": [
    "hashcat/jobs/crack-2024-abc123/input/rules/best64.rule",
    "hashcat/jobs/crack-2024-abc123/input/rules/toggles.rule"
  ],
  "attack_mode": 0,
  "hash_type": 0,
  "mask": "",
  "skip": 0,
  "limit": 200000,
  "chunk_idx": 0,
  "total_chunks": 5,
  "extra_args": "--force",
  "restore_file_s3": ""
}
```

- `rule_files_s3`: Array of S3 keys pointing to rule files. Multiple rule files are supported (stacking). When empty or absent, no rules are applied. Each worker downloads all rule files and passes them as `-r` flags to hashcat in order.
- When a task is re-queued after spot termination, `restore_file_s3` points to the saved session in S3, `skip`/`limit` are updated to reflect remaining work, and `rule_files_s3` is preserved (rule files persist in S3 for the job's lifetime).

### Sidecar Container

The sidecar runs alongside the hashcat worker on each EC2 spot instance. Its only job is to detect termination and trigger a graceful save.

**Termination detection:**
```
Poll http://169.254.169.254/latest/meta-data/spot/termination-time
  - 404 → not scheduled, keep polling (every 5 seconds)
  - 200 → termination scheduled, trigger save
```

**Save process (on termination notice):**
1. Read hashcat PID from shared file (`/shared/hashcat.pid`)
2. Send `SIGUSR1` to hashcat process (triggers checkpoint)
3. Wait for `.restore` file to appear (hashcat writes it)
4. Upload `.restore` file to S3
5. Calculate remaining keyspace (original skip+limit minus progress)
6. Send new SQS message with remaining work, `restore_file_s3` set, and `rule_files_s3` preserved
7. Exit (instance will be terminated within ~2 minutes)

**Communication between containers:**
- Shared volume mounted at `/shared/`
- Hashcat worker writes PID to `/shared/hashcat.pid` on startup
- Hashcat worker writes progress to `/shared/progress` periodically
- Sidecar reads these files to coordinate save/re-queue

### Hashcat Worker

```
1. Receive SQS message
2. Download hash file + wordlist from S3
3. Download rule files from S3 to /data/rules/ (if rule_files_s3 is non-empty)
4. If restore_file_s3 is set, download restore file
5. Write PID to /shared/hashcat.pid
6. Run hashcat:
   hashcat --session=job_id \
           --checkpoint-timer=60 \
           -a {attack_mode} \
           -m {hash_type} \
           --skip {skip} \
           --limit {limit} \
           [-r /data/rules/best64.rule [-r /data/rules/toggles.rule ...]] \
           -o /shared/output.txt \
           {hash_file} {wordlist_or_mask}
   OR if restoring:
   hashcat --session=job_id --restore
7. On completion: upload results to S3, delete SQS message
8. On error: log error, message returns to queue via visibility timeout
```

Rule files are passed via `-r` flags in the order specified in `rule_files_s3`. The `--skip`/`--limit` values already account for the rules-expanded keyspace.

### EC2 Spot Instance Setup

**Launch Template:**
- AMI: Deep Learning AMI (Ubuntu) — pre-installed NVIDIA drivers
- Instance types: `g4dn.xlarge` (1 T4 GPU, cheapest), `g4dn.2xlarge`, `g5.xlarge` (1 A10G)
- User data: pulls Docker images, starts hashcat worker + sidecar containers
- IAM instance profile: SQS read/delete, S3 read/write

**Auto Scaling Group (ASG):**
- Mixed instances policy with spot allocation
- Desired capacity = number of chunks (or user-configured max)
- Min: 0, Max: user-configured
- Spot allocation strategy: `capacity-optimized` (best availability)
- No scaling policies — ASG is created for a job and destroyed after

**Lifecycle:**
1. TUI triggers Terraform to create ASG + launch template
2. ASG launches spot instances
3. Instances pull from SQS, process chunks
4. As chunks complete, instances have no more work and idle
5. TUI monitors S3 for results, shows progress
6. User triggers cleanup (Terraform destroy)

### S3 Key Structure (Hashcat)

```
hashcat/jobs/{job_id}/
├── input/
│   ├── hashes.txt
│   ├── wordlist.txt (or mask stored in SQS message)
│   └── rules/
│       ├── best64.rule
│       └── toggles.rule
├── sessions/
│   └── chunk{N}.restore     # Saved sessions from interrupted spots
├── results/
│   └── chunk{N}_output.txt  # Cracked hashes per chunk
└── status.json              # Job metadata and progress
```

## Cloud Provider Abstraction

### Interfaces (`internal/cloud/provider.go`)

```go
type Provider interface {
    Storage() Storage
    Queue() Queue
    Compute() Compute
}

type Storage interface {
    Upload(ctx context.Context, bucket, key string, data []byte) error
    Download(ctx context.Context, bucket, key string) ([]byte, error)
    List(ctx context.Context, bucket, prefix string) ([]string, error)
}

type Queue interface {
    Send(ctx context.Context, queueID string, body string) error
    Receive(ctx context.Context, queueID string) (*Message, error)
    Delete(ctx context.Context, queueID string, receiptHandle string) error
}

type Compute interface {
    RunContainer(ctx context.Context, opts ContainerOpts) (string, error)
    RunSpotInstances(ctx context.Context, opts SpotOpts) ([]string, error)
    GetSpotStatus(ctx context.Context, instanceIDs []string) ([]SpotStatus, error)
}
```

AWS implements these via the existing SDK wrappers. GCP and Azure would provide their own implementations (Cloud Storage/GCS, Pub/Sub, GCE preemptible VMs, etc.).

The TUI and tool logic (nmap, hashcat) only depend on these interfaces, never on AWS-specific code directly.

## Networking Enhancements (IP Diversity)

Scans from a single NAT Gateway IP get flagged and rate-limited by SOC teams and AWS abuse detection. IP diversity is critical for operational scanning. All options are user-configurable via CLI flags and TUI toggles.

### IPv6 Support

**Decision: Dual-Stack VPC + Egress-Only Internet Gateway (EIGW)**

The shared networking module deploys a dual-stack VPC so Fargate tasks can scan both IPv4 and IPv6 targets from the same container.

```
Private Subnets (dual-stack: IPv4 + IPv6 CIDR)
      │
      ├── IPv4 traffic → NAT Gateway → Internet (existing)
      │
      └── IPv6 traffic → Egress-Only Internet Gateway → Internet (new, free)
```

**Why this approach:**
- EIGW has **no hourly charge** (unlike NAT Gateway)
- EIGW is stateful — allows outbound IPv6 but blocks unsolicited inbound
- Minimal Terraform changes: add IPv6 CIDR to VPC, create EIGW, add `::/0 → EIGW` route
- Fargate tasks get both IPv4 and IPv6 addresses, so scanning either protocol is just an nmap flag (`-6`)
- All existing IPv4 infrastructure remains untouched

**Terraform changes (in `deployments/aws/shared/networking/`):**
- Add `assign_generated_ipv6_cidr_block = true` to VPC
- Add `ipv6_cidr_block` to each subnet
- Create `aws_egress_only_internet_gateway` resource
- Add `::/0 → EIGW` route to private route tables
- Update security groups to allow IPv6 egress

**Prerequisite:** The `dualStackIPv6` ECS account setting must be enabled via AWS CLI:
```bash
aws ecs put-account-setting-default --name dualStackIPv6 --value enabled
```
This is a one-time account-level toggle, not a Terraform resource. The TUI/CLI `infra deploy` command should check this and prompt the user.

**Tool support:**
- Nmap: full IPv6 via `-6` flag
- Naabu: IPv6 via `-ip-version 6` (experimental)
- ffuf: IPv6 depends on target URL resolution
- Hashcat: N/A (not network-dependent)

### Multi-NAT Gateway for IP Diversity

By default, all Fargate tasks egress through a single NAT Gateway (single IP). With `multi_nat` enabled, each AZ gets its own NAT Gateway with a unique Elastic IP, distributing scan traffic across multiple source IPs.

```
Private Subnets (3 AZs)
      │
      ├── AZ-a → NAT Gateway A (EIP: 54.x.x.1) → Internet
      ├── AZ-b → NAT Gateway B (EIP: 54.x.x.2) → Internet
      └── AZ-c → NAT Gateway C (EIP: 54.x.x.3) → Internet
```

**Why this matters:** A single source IP running aggressive scans gets flagged quickly. Multiple NAT Gateways spread traffic across IPs, reducing per-IP scan volume and detection risk.

**Trade-off:** Each NAT Gateway costs $0.045/hr. With 3 AZs, that's $0.135/hr vs $0.045/hr for a single NAT. Reinforces the "tear down after each job" pattern.

**Configuration:** `--multi-nat` CLI flag, TUI toggle. Terraform variable `multi_nat = true/false`.

### Selfhosted Provider: Networking is Free

The AWS networking enhancements above (EIGW, multi-NAT) solve problems that don't exist on selfhosted VPS providers:

- **IPv6:** Most VPS providers (Hetzner, Vultr, Linode, Scaleway) assign native IPv6 (often a /64 block) to every VM by default. No dual-stack VPC or EIGW needed — worker VMs have IPv6 out of the box. Scanning with `-6` just works.
- **IP diversity:** Each VM gets its own unique public IPv4 and IPv6 address. Spinning up 10 workers = 10 unique source IPs automatically. There's no shared NAT Gateway bottleneck to work around.

This is a key reason selfhosted is higher priority than GCP/Azure — red teamers get better IP diversity at lower cost with zero networking infrastructure.

**Terraform requirements for selfhosted (PR 9.3):**
- Firewall rules must explicitly allow IPv6 egress (Hetzner Cloud Firewalls default-deny)
- No NAT or EIGW resources needed
- Private networking between workers and controller (NATS) can be IPv4-only

**`NetworkingConfig` must be provider-aware (PR 10.3):**
- On AWS: controls EIGW toggle, multi-NAT toggle, displays NAT Gateway IPs
- On selfhosted: mostly informational — displays per-VM public IPs (v4 + v6), no toggles needed since diversity is inherent
- The `--enable-ipv6` CLI flag should work across providers: on AWS it triggers EIGW deployment, on selfhosted it's a no-op (or a validation check that VMs have IPv6 assigned)

## Infrastructure Organization

```
deployments/
└── aws/
    ├── shared/                  # Shared modules
    │   ├── networking/          # VPC, subnets, NAT, EIGW, multi-NAT
    │   └── security/            # Base IAM roles
    ├── nmap/                    # Nmap-specific infra (existing)
    │   ├── environments/dev/
    │   ├── compute/             # ECS cluster, Fargate tasks
    │   ├── messaging/           # SQS
    │   ├── spot/                # EC2 Spot Fleet IAM + AMI
    │   └── storage/             # S3
    └── generic/                 # Generic module infra (parameterized)
        ├── environments/dev/
        ├── compute/             # ECS cluster, Fargate tasks (cpu/memory vars)
        ├── messaging/           # SQS queue + DLQ
        └── storage/             # S3 bucket
```

The generic module replaces per-tool Terraform for standard tools. Deploy with `tool_name`, `container_image`, `cpu`, `memory`, and `max_concurrency` variables. Networking is shared between tools (same VPC).

## Container Images

Images are pushed to ECR (one repository per tool, created by Terraform). The `heph infra deploy` and TUI deploy flow handle building and pushing images as part of the deployment process.

### Generic Worker (`containers/generic/Dockerfile`)
```
Base: golang:1.26-alpine (builder) → alpine:3.19 (runtime)
Build arg: TOOL_INSTALL_CMD (e.g., "go install .../nuclei@latest", "apk add masscan")
User: non-root
Entrypoint: generic worker binary (reads TOOL_NAME env, loads module definition, processes tasks)
```

Per-tool images are built from the same Dockerfile with different `TOOL_INSTALL_CMD` values. See `IMPLEMENTATION.md` Phase 5 for details.

### Future: Hashcat Worker (`containers/hashcat/Dockerfile`)
```
Base: nvidia/cuda:12.x-runtime-ubuntu22.04
Install: hashcat, aws-cli
User: non-root
Entrypoint: download inputs (hashes, wordlist, rule files) from S3, run hashcat with -r flags, upload results
Volumes: /shared/ (shared with sidecar), /data/rules/ (downloaded rule files)
```

### Future: Hashcat Sidecar (`containers/hashcat-sidecar/Dockerfile`)
```
Base: alpine:3.19
Install: aws-cli (or just curl + minimal tooling)
User: non-root
Entrypoint: poll metadata endpoint, trigger save on termination
```

Both hashcat containers share a volume at `/shared/` for PID file, progress, and restore files.

## Implementation Phases

1. **Phase 1: Project restructure** — reorganize directories, update go.mod module name, migrate existing nmap code into new structure
2. **Phase 2: TUI + CLI skeleton** — Bubbletea app with main menu, navigation, and settings view; CLI entry point with subcommands
3. **Phase 3: Cloud interfaces + nmap integration** — cloud provider interfaces, infrastructure lifecycle package, nmap TUI views, EC2 Spot Fleet, CLI, binary rename (`heph`)
4. **Phase 4: Nmap enhancements** — port parsing/splitting (per WORKLOAD_DISTRIBUTION.md) + scan hardening (retry logic, jitter, DNS options, reverse DNS toggle)
5. **Phase 5: Generalized module system** — module definitions (YAML), generic worker binary, generic Terraform module, all tools (including nmap) on the generic backend, target-list and wordlist/fuzzer UX
6. **Phase 6: Tool expansion** — per-tool container images, wordlist splitting for ffuf/gobuster, result formatters
7. **Phase 7: Naabu + Nmap pipeline** — naabu module with `-nmap-cli` combined scanning, TUI/CLI views
8. **Phase 8: Result export** — CLI export commands (`heph results`), result formatters per tool, `--format json` for pipeline integration
9. **Phase 9: Selfhosted provider** — NATS JetStream queue, S3-compatible storage, Docker-on-VMs compute, Hetzner Terraform module. One implementation covers any VPS provider
10. **Phase 10: Networking enhancements** — dual-stack VPC with EIGW, multi-NAT Gateway for IP diversity, user-configurable networking
- **Future: Hashcat** — GPU cracking with spot interruption handling (deprioritized, design preserved above)
- **Future: Big 3 multi-cloud** — GCP and Azure provider implementations (deprioritized; selfhosted provider covers VPS providers)

See `IMPLEMENTATION.md` for detailed PR and commit breakdowns per phase.
