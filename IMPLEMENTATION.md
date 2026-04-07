# Implementation Plan

Breakdown of the ARCHITECTURE.md into phases, PRs, and commits.

10 active phases, 39 PRs. Hashcat and Big 3 multi-cloud (GCP, Azure) are deferred to Future/Deprioritized.

## Testing Strategy

### Go Unit Tests

Every new package gets a `_test.go` file. Tests use the standard `testing` package.

| Package | What to test |
|---|---|
| `internal/tools/nmap/portparse.go` | Port spec parsing, splitting, formatting, edge cases |
| `internal/tools/nmap/scanner.go` | Target parsing, mode selection, task generation |
| `internal/tools/nmap/errors.go` | Error classification (permanent, transient, partial) |
| `internal/tools/nmap/jitter.go` | Jitter delay, timing template injection |
| `internal/tools/nmap/dns.go` | Resolver assignment, DNS flag building |
| `internal/tools/naabu/scanner.go` | Target parsing, result-to-nmap-task conversion |
| `internal/modules/module.go` | Module definition parsing, validation, defaults |
| `internal/modules/registry.go` | Registry lookup, listing, embedded YAML loading |
| `internal/worker/generic.go` | Command template substitution, timeout handling, retry classification |
| `internal/worker/jitter.go` | Shared jitter utilities |
| `internal/worker/errors.go` | Shared failure classification types |
| `internal/infra/terraform.go` | Terraform lifecycle operations (init, plan, apply, destroy output parsing) |
| `internal/infra/docker.go` | Docker build, tag, push command construction |
| `internal/cloud/provider.go` | Mock provider for testing tool logic without AWS |
| `internal/cloud/aws/*.go` | AWS client wrappers tested against mocked SDK interfaces |
| `internal/config/config.go` | Env var loading, validation, defaults |

**Pattern:** Use interfaces for AWS clients so they can be mocked in tests. Example:

```go
// internal/cloud/aws/sqs.go
type SQSAPI interface {
    SendMessage(ctx context.Context, params *sqs.SendMessageInput, ...) (*sqs.SendMessageOutput, error)
    ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, ...) (*sqs.ReceiveMessageOutput, error)
    DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, ...) (*sqs.DeleteMessageOutput, error)
}

// In tests, provide a mock implementation of SQSAPI
```

### Terraform Validation

| Level | Tool | What it checks |
|---|---|---|
| Syntax | `terraform validate` | HCL syntax, required variables, module references |
| Plan | `terraform plan` | Resource graph, no errors, expected resource count |
| Lint | `tflint` | Best practices, deprecated syntax, naming conventions |
| Security | `tfsec` or `trivy` | Security misconfigurations (open SGs, missing encryption, etc.) |

Add a `Makefile` target and CI job:

```makefile
tf-validate:
	cd deployments/aws/nmap/environments/dev && terraform init -backend=false && terraform validate
	cd deployments/aws/generic/environments/dev && terraform init -backend=false && terraform validate

tf-lint:
	tflint --recursive deployments/

tf-security:
	trivy config deployments/
```

### Integration Tests

Run against real AWS infrastructure in a dedicated test account/environment.

| Test | What it verifies |
|---|---|
| Nmap end-to-end | Deploy infra → submit scan (scanme.nmap.org) → verify result in S3 → destroy |
| Generic worker end-to-end | Deploy generic infra for nuclei → submit scan → verify result in S3 → destroy |
| Naabu + Nmap pipeline | Deploy infra → run naabu+nmap combined scan → verify results → destroy |
| IPv6 scan | Deploy dual-stack infra → scan IPv6 target → verify result |

Integration tests live in `tests/integration/` and are gated behind a build tag:

```go
//go:build integration

package integration
```

Run with: `go test -tags=integration ./tests/integration/...`

### CI Pipeline (GitHub Actions)

```
on: [push, pull_request]

jobs:
  unit-tests:     go test ./...
  tf-validate:    terraform validate + tflint + trivy
  build:          go build all cmd/ targets
  docker-build:   docker build all containers/
  integration:    (manual trigger only, needs AWS creds)
```

---

## Phase 1: Project Restructure

Reorganize the repository from the current flat structure into the new multi-tool layout.

### PR 1.1: Rename module and restructure directories ✅

**Commits:**

1. **Rename Go module from `nmap-scanner` to `heph4estus`**
   - Update `go.mod` module name
   - Update all import paths in every `.go` file

2. **Move nmap code into new directory structure**
   - `cmd/producer/main.go` → `cmd/heph/main.go` (temporary, will be refactored)
   - `cmd/consumer/main.go` → `cmd/workers/nmap/main.go`
   - `internal/scanner/` → `internal/tools/nmap/`
   - `internal/aws/` → `internal/cloud/aws/`
   - `internal/models/` → `internal/tools/nmap/models.go`
   - `internal/config/` and `internal/logger/` stay
   - Update all import paths

3. **Move Terraform into deployments/**
   - `terraform/modules/` → `deployments/aws/nmap/`
   - `terraform/environments/` → `deployments/aws/nmap/environments/`
   - Extract networking and security into `deployments/aws/shared/`

4. **Move Dockerfile into containers/**
   - `Dockerfile` → `containers/nmap/Dockerfile`
   - Update any build scripts or docs referencing it

5. **Update documentation**
   - Update `README.md` with new paths
   - Update `CLAUDE.md` with new structure

### PR 1.2: Add Makefile and build tooling ✅

**Commits:**

1. **Add Makefile with build targets**
   - `make build` — build all cmd/ binaries
   - `make test` — run unit tests
   - `make docker-build` — build all container images
   - `make tf-validate` — validate all Terraform
   - `make lint` — run golangci-lint
   - `make clean` — remove build artifacts

2. **Add golangci-lint config (`.golangci.yml`)**

3. **Add `.github/workflows/ci.yml`**
   - Unit tests, build, lint, Terraform validate on push/PR

---

## Phase 2: TUI + CLI Skeleton

### PR 2.1: Add Bubbletea dependencies and TUI skeleton ✅

**Commits:**

1. **Add Charm dependencies to go.mod**
   - `go get github.com/charmbracelet/bubbletea`
   - `go get github.com/charmbracelet/bubbles`
   - `go get github.com/charmbracelet/lipgloss`
   - `go get github.com/charmbracelet/huh`

2. **Create root TUI model (`internal/tui/app.go`)**
   - `App` struct implementing `tea.Model`
   - `View` interface for swappable views
   - Window resize handling

3. **Create styles (`internal/tui/styles.go`)**
   - Lipgloss styles for borders, colors, layout
   - Consistent theme across all views

4. **Create main menu view (`internal/tui/views/menu/model.go`)**
   - List of tools: Nmap Scanner, Naabu + Nmap, Settings
   - Keyboard navigation (j/k or arrows, enter to select, q to quit)

5. **Create settings view (`internal/tui/views/settings/model.go`)**
   - AWS credentials display/configuration
   - Region selection
   - Default options

6. **Create TUI entry point (`cmd/heph4estus/main.go`)**
   - Initialize `tea.Program`, run root model

7. **Add unit tests for TUI model navigation**
   - Test view switching on selection
   - Test quit handling

### PR 2.2: CLI entry point with subcommands ✅

**Commits:**

1. **Create CLI entry point (`cmd/heph/main.go`)**
   - Subcommand structure: `heph nmap`, `heph scan --tool <name>`, `heph infra`, `heph status`
   - `--format json|text` output flag
   - `--help` for each subcommand

2. **Create shared job runner (`internal/jobs/runner.go`)**
   - Common interface that both TUI and CLI use to launch/monitor jobs
   - Accepts tool config, cloud provider, returns job status

3. **Add unit tests for CLI argument parsing**

---

## Phase 3: Cloud Interfaces + Nmap Integration

### PR 3.1: Cloud provider interfaces and AWS implementation ✅

**Commits:**

1. **Define cloud provider interfaces (`internal/cloud/provider.go`)**
   - `Provider`, `Storage`, `Queue`, `Compute` interfaces
   - `Message`, `ContainerOpts`, `SpotOpts`, `SpotStatus` types

2. **Implement AWS provider (`internal/cloud/aws/provider.go`)**
   - Wrap existing SQS, S3 clients behind new interfaces
   - Add SDK interface types for mockability

3. **Add mock provider for testing (`internal/cloud/mock/`)**

4. **Add unit tests for AWS provider wrappers**
   - Mock AWS SDK interfaces
   - Test error handling, retries

### PR 3.2: Infrastructure lifecycle package ✅

**Commits:**

1. **Create `internal/infra/terraform.go`**
   - `Init(workDir string) error`
   - `Plan(workDir string, vars map[string]string) (string, error)` — returns plan summary
   - `Apply(workDir string, vars map[string]string, stream io.Writer) error`
   - `Destroy(workDir string, stream io.Writer) error`
   - `ReadOutputs(workDir string) (map[string]string, error)`
   - Shells out to `terraform` binary with correct working directory

2. **Create `internal/infra/docker.go`**
   - `Build(dockerfile, context, tag string, stream io.Writer) error`
   - `Tag(source, target string) error`
   - `Push(tag string, stream io.Writer) error`

3. **Create `internal/infra/ecr.go`**
   - `Authenticate(region string) error` — ECR login
   - `GetRepoURI(region, repoName string) (string, error)`

4. **Add unit tests for command construction and output parsing**

### PR 3.3: Nmap TUI views (Worker Pool Architecture) ✅

Uses a **worker pool model** instead of SFN Map state: batch-enqueue all targets to SQS,
launch N long-running Fargate workers that poll/scan/upload in a loop, track progress via
S3 object count. Scales to hundreds of thousands of targets.

**Changes:**

1. **Core types** — `NavigateWithDataMsg`, `DeployConfig`, `InfraOutputs`, `StageCompleteMsg`, `TickMsg`, `StreamWriter`
2. **Cloud interfaces** — `Queue.SendBatch`, `Storage.Count`, `ContainerOpts.TaskDefinition/Count`, paginated `List`, real ECS `RunContainer`
3. **Worker loop refactor** — `cmd/workers/nmap/main.go` now loops until queue empty, uses cloud interfaces, fixes retry (delete only after S3 upload)
4. **Nmap config view** — form with target file, nmap options, worker count
5. **Deploy view** — multi-stage async pipeline (init → plan → approve → apply → outputs → build → ECR → push)
6. **Nmap status view** — three-phase UI (enqueue → launch workers → poll S3 progress with rate/ETA)
7. **Nmap results view** — paginated table with on-demand detail download
8. **App wiring** — Nmap Scanner enabled in menu, `NavigateWithDataMsg` handling
9. **`ProgressCounter` interface** (`internal/cloud/provider.go`) — Abstraction for progress tracking with auto-threshold plan for switching compute modes
10. **`ComputeMode` field on `DeployConfig`** — Supports "auto" (default), "fargate", and "spot" modes for compute selection

### PR 3.6: EC2 Spot Fleet Support ✅

Adds dual compute mode — Fargate for small jobs, EC2 Spot Fleet for large-scale scanning.

**Changes:**

1. **EC2Client** (`internal/cloud/aws/ec2.go`) — `RunSpotInstances` using CreateFleet API with `capacity-optimized-prioritized` allocation, `GetSpotStatus` via DescribeInstances
2. **CompositeCompute** (`internal/cloud/aws/composite.go`) — Delegates `RunContainer`→ECS, `RunSpotInstances`→EC2. Implements `cloud.Compute` interface
3. **UserData generator** (`internal/cloud/aws/userdata.go`) — Generates base64-encoded bootstrap script: install Docker, ECR login, pull image, run container, self-terminate
4. **Compute interface** (`internal/cloud/provider.go`) — Extended with `RunSpotInstances(ctx, SpotOpts)` and `GetSpotStatus(ctx, instanceIDs)`. Added `SpotOpts`, `SpotStatus`, `ProgressCounter` interface, `ErrNotImplemented`
5. **ContainerName fix** (`internal/cloud/aws/ecs.go`) — Container name for env overrides now configurable via `ContainerOpts.ContainerName` (defaults to "worker")
6. **Auto-select compute mode** — `SpotThreshold = 50` in nmap status view. `ComputeMode` field: "auto" (default), "fargate", "spot"
7. **Terraform spot module** (`deployments/aws/nmap/spot/`) — IAM instance profile with SQS/S3/ECR/self-terminate permissions, AL2023 AMI data source

### PR 3.4: Nmap CLI subcommand ✅

**Commits:**

1. **Implement `heph nmap` subcommand**
   - Flags: `--file`, `--mode`, `--default-options`, `--port-chunks`
   - Current shipped behavior: read existing infra outputs, then submit → monitor → collect
   - Shared auto-deploy / reuse lifecycle is promoted into Phase 5 operator UX work (PR 5.10-5.11)
   - Outputs progress to stdout, results as JSON

2. **Implement `heph infra deploy --tool nmap`**
   - Runs `terraform plan`, shows summary, prompts for approval
   - `--auto-approve` flag to skip prompt (for CI/CD pipelines)
   - On approval: runs `terraform apply`, builds and pushes container image
   - Reads and displays Terraform outputs (ARNs, URLs)
   - Uses `internal/infra/` package

3. **Implement `heph infra destroy --tool nmap`**
   - Empties S3 bucket, runs `terraform destroy`
   - `--auto-approve` flag to skip prompt

4. **Add unit tests for nmap CLI flag parsing and validation**

### PR 3.5: Consumer refactor to use cloud interfaces ✅ (absorbed into PR 3.3)

Worker refactored to use `cloud.Queue`/`cloud.Storage` interfaces, poll loop,
and proper retry semantics as part of PR 3.3 changes.

### PR 3.7: Rename CLI binary to `heph` ✅

Renamed `cmd/heph-cli/` to `cmd/heph/`, updated build targets, docs, and all references. `heph4estus` remains the TUI binary name.

---

## Phase 4: Nmap Enhancements

Port splitting and scan hardening. Consolidates old Phase 4 (workload distribution) and old Phase 6 (scan hardening) into a single phase.

### PR 4.1: Port parsing library ✅

**Commits:**

1. **Create `internal/tools/nmap/portparse.go`** (co-located with nmap tool logic)
   - `ParsePortSpec(spec string) ([]int, error)`
   - `SplitPorts(ports []int, chunks int) [][]int`
   - `FormatPortSpec(ports []int) string`
   - `ExtractPortFlag(options string) (portSpec, remainingOptions string, found bool)`

2. **Add comprehensive unit tests (`internal/tools/nmap/portparse_test.go`)**
   - Table-driven tests: single ports, ranges, mixed, all-ports, invalid input
   - Split tests: even/uneven division, more chunks than ports
   - Format tests: range collapsing

### PR 4.2: Integrate port splitting into scanner and producer ✅

**Commits:**

1. **Add `GroupID`, `ChunkIdx`, `TotalChunks` to nmap models**

2. **Add `ParseTargetsWithMode` to scanner**
   - Calls portparse when mode is `target-ports`
   - Defaults to all ports (1-65535) when no `-p` flag

3. **Update producer to use new mode flags**
   - Add `-mode` and `-port-chunks` to both TUI config and CLI flags
   - Add payload size validation (256KB SQS message limit)

4. **Update consumer S3 key generation for group-prefixed paths**

5. **Add unit tests for ParseTargetsWithMode**

### PR 4.3: Scan hardening (retry logic, jitter, optional DNS passthrough) ✅

Consolidates what was previously three separate PRs (old Phase 6) into one.

**Commits:**

1. **Create `internal/tools/nmap/errors.go`**
   - `ClassifyError(err error, output string) ErrorClass` — returns `Permanent`, `Transient`, or `PartialSuccess`
   - Permanent: invalid target, permission denied, malformed options
   - Transient: DNS resolution timeout, host unreachable, network flap
   - PartialSuccess: some hosts scanned, some failed

2. **Modify `cmd/workers/nmap/main.go` for retry logic**
   - Stop unconditionally deleting SQS messages
   - Delete on success or permanent failure only
   - Let transient failures return to queue via SQS visibility timeout
   - Add `Warnings []string` to `ScanResult`

3. **Create `internal/tools/nmap/jitter.go`**
   - `ApplyJitter(ctx context.Context, maxDelaySeconds int) error` — random delay using `crypto/rand`
   - Add `JITTER_MAX_SECONDS` and `NMAP_TIMING_TEMPLATE` env vars
   - Inject `-T` timing flag if `NMAP_TIMING_TEMPLATE` is set

4. **Add `--dns-servers` CLI flag to `cmd/heph/cmd_nmap.go`**
   - Optional passthrough: user-specified DNS resolvers forwarded to nmap via `--dns-servers`
   - No automatic distribution or round-robin — users specify resolvers explicitly when needed
   - Useful for internal engagements with specific DNS infrastructure
   - Passed through to workers via `ScanTask.DnsServers` field (omitempty)
   - Worker injects `--dns-servers` into nmap command when field is set

5. **Update Terraform compute module with new env vars**
   - `JITTER_MAX_SECONDS`, `NMAP_TIMING_TEMPLATE`

6. **Add unit tests for error classification, jitter, and DNS passthrough**

### PR 4.4: Disable reverse DNS (`--no-rdns` flag) ✅

Add a first-class CLI flag to disable nmap's reverse DNS resolution. Reverse DNS is the primary source of DNS load in IP-based scans. Disabling it significantly speeds up scans and reduces DNS fingerprinting. Users can already do `--default-options "-sS -n"` but a dedicated flag improves discoverability.

**Commits:**

1. **Add `--no-rdns` flag to `cmd/heph/cmd_nmap.go`**
   - When set, injects `-n` into nmap options for all tasks
   - Works with both `target-only` and `target-ports` modes

2. **Add TUI checkbox for reverse DNS in `internal/tui/views/nmap/config.go`**
   - Thread through `DeployConfig` and `InfraOutputs`

3. **Add unit tests for `-n` injection**

---

## Phase 5: Generalized Module System

The core insight from COMPARISON.md: ~90% of axiom's 86+ tools follow the same "input file → run command → output file" pattern. Instead of building custom Go workers and Terraform for each tool, define tools as simple module definitions and use a generic worker + generic Terraform module.

Phase 5 now also closes the main operator-experience gap before provider expansion: the generic runtime should not require users to think about Terraform directories, backend choice, or whether they remembered to run `heph infra deploy` first.

### PR 5.1: Module definition format and registry ✅

**Commits:**

1. **Create `internal/modules/module.go`**
   - Module definition struct:

```go
type ModuleDefinition struct {
    Name          string            `yaml:"name"`
    Description   string            `yaml:"description"`
    Exec          []string          `yaml:"exec"`          // argv template with {{input}}, {{output}}, {{target}}, {{wordlist}}, {{options}}
    Shell         string            `yaml:"shell,omitempty"` // explicit escape hatch for tools that truly require shell semantics
    InputType     string            `yaml:"input_type"`    // "target_list" or "wordlist"
    OutputExt     string            `yaml:"output_ext"`
    InstallCmd    string            `yaml:"install_cmd"`   // for generic Dockerfile
    DefaultCPU    int               `yaml:"default_cpu"`   // Fargate CPU units (256, 512, 1024, etc.)
    DefaultMemory int               `yaml:"default_memory"` // Fargate memory in MB
    Timeout       string            `yaml:"timeout"`
    Tags          []string          `yaml:"tags"`
    Env           map[string]string `yaml:"env,omitempty"` // extra env vars for the container
}
```

   - Validation: required fields, valid input types, valid timeout format, `exec`/`shell` mutual exclusion

2. **Create `internal/modules/registry.go`**
   - `Registry` struct holding all loaded module definitions
   - `Get(name string) (*ModuleDefinition, error)`
   - `List() []ModuleDefinition`
   - `ListByTag(tag string) []ModuleDefinition`

3. **Create built-in YAML module definitions embedded via `//go:embed`**
   - Directory: `internal/modules/definitions/`
   - Built-in modules:

```yaml
# internal/modules/definitions/nmap.yaml
name: nmap
description: Network port scanner and service detection
exec: ["nmap", "{{options}}", "-oX", "{{output}}", "{{target}}"]
input_type: target_list
output_ext: xml
install_cmd: "apk add --no-cache nmap nmap-scripts"
default_cpu: 256
default_memory: 512
timeout: 5m
tags: [scanner, network]
```

```yaml
# internal/modules/definitions/nuclei.yaml
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

```yaml
# internal/modules/definitions/ffuf.yaml
name: ffuf
description: Web fuzzer for directories, vhosts, parameters
exec: ["ffuf", "-w", "{{input}}", "-u", "{{target}}", "-of", "json", "-o", "{{output}}", "-ac"]
input_type: wordlist
output_ext: json
install_cmd: "go install github.com/ffuf/ffuf/v2@v2.1.0"
default_cpu: 256
default_memory: 512
timeout: 30m
tags: [fuzzer, web]
```

   - Additional built-in modules: subfinder, httpx, masscan, gobuster, feroxbuster, dnsx, katana, gospider, massdns, dalfox, gowitness

4. **Add unit tests for module parsing, validation, and registry lookup**

### PR 5.2: Generic worker binary and container ✅

**Commits:**

1. **Create `cmd/workers/generic/main.go`**
   - Reads `TOOL_NAME` env var to determine which module definition to load
   - Flow:
     1. Load module definition from embedded registry
     2. Pull task from queue (SQS)
     3. Download input file(s) from storage (S3)
     4. Render argv template: substitute `{{input}}`, `{{output}}`, `{{target}}`, `{{wordlist}}`, `{{options}}`
     5. Execute command with configured timeout
     6. Upload output file to storage (S3)
     7. Classify result (success/transient/permanent failure)
     8. Acknowledge or leave message based on classification
   - Uses `internal/cloud/provider.go` interfaces (same as refactored nmap worker)
   - Uses `internal/worker/` package for shared jitter and error classification

2. **Extract shared worker utilities** (if not already done)
   - Create `internal/worker/jitter.go` (extracted from `internal/tools/nmap/jitter.go`)
   - Create `internal/worker/errors.go` (shared `ErrorClass` type and failure classification)
   - Refactor nmap worker to use shared `internal/worker/` package

3. **Create `containers/generic/Dockerfile`**
   - Multi-stage build with `TOOL_INSTALL_CMD` build arg:

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

   - Build per-tool images: `docker build --build-arg TOOL_INSTALL_CMD="apk add nmap" -t scanner-nmap containers/generic/`

4. **Add unit tests for generic worker logic**
   - Command template substitution
   - Timeout enforcement
   - Error classification

### PR 5.3: Generic Terraform module ✅

**Commits:**

1. **Create `deployments/aws/generic/` module**
   - Parameterized by `tool_name`, `container_image`, `cpu`, `memory`, `concurrency`
   - Composes shared networking + security modules (same as nmap)
   - Resources:
     - ECS cluster + Fargate task definition (cpu/memory from variables)
     - SQS queue + DLQ
     - S3 bucket for inputs/outputs
     - ECR repository
     - IAM roles (ECS execution, ECS task)
   - Structure:

```
deployments/aws/generic/
├── compute/         # ECS cluster, Fargate task definition
├── messaging/       # SQS queue + DLQ
├── storage/         # S3 bucket
├── environments/dev/
│   ├── main.tf      # Module composition with tool_name variable
│   ├── variables.tf
│   └── outputs.tf
```

2. **Add `make tf-validate-generic` target**

3. **Add Terraform validate + tfsec tests for generic module**

### PR 5.4: Refactor nmap to use generic system ✅

**What was delivered:**

1. **Fixed Terraform outputs** — Renamed `ecr_repository_url` → `ecr_repo_url` in both nmap and generic environments to match Go code. Added missing `task_definition_arn`, `security_group_id`, `subnet_ids` outputs.

2. **Moved nmap option injection to producer side** — `--timing-template`, `--dns-servers`, and `--no-rdns` are now injected into task options at enqueue time, keeping the generic worker tool-agnostic.

3. **Added `--use-generic` flag to `heph nmap`** — Enqueues `worker.Task` with correct container name (`nmap-worker`), env vars (`TOOL_NAME=nmap`), and S3 prefix (`scans/nmap/`). Both dedicated and generic paths coexist.

4. **Added `--backend generic` flag to `heph infra`** — Routes to generic Terraform with `tool_name=nmap` variable and generic Dockerfile with `RUNTIME_INSTALL_CMD` build arg. Added `BuildWithArgs()` to `DockerClient`.

5. **Added `docker-build-nmap-generic` Makefile target** and tests for `resolveToolPaths` and `extractTargetFromKey` with generic S3 paths.

### PR 5.5: Generic execution correctness and hardening ✅

**What was delivered:**

1. **Added first-class `job_id` scoping** — nmap and generic task/result payloads now carry `job_id`, and result storage moved to job-scoped prefixes:
   - `scans/{tool}/{job_id}/results/...`
   - `scans/{tool}/{job_id}/artifacts/...`

2. **Fixed generic progress counting** — CLI and TUI progress now count only result records under the job-scoped `results/` prefix instead of all objects under a shared `scans/` namespace.

3. **Populated `OutputKey` in generic results** — generic workers upload artifacts first and persist the exact artifact path in the structured result JSON.

4. **Hardened generic execution** — built-in modules now execute via argv-style `exec` templates by default, with safe `{{options}}` tokenization and explicit `shell` opt-in only for modules that truly require shell semantics.

5. **Tightened reproducibility** — pinned Go-installed tool versions in built-in module definitions and aligned builder images with Go 1.26.

### PR 5.6: Wire target-list generic modules into TUI and CLI ✅

**Commits:**

1. **Update TUI main menu to dynamically populate from module registry**
   - Replace hardcoded tool list with `registry.List()`
   - Each module gets a menu entry
   - `target_list` modules are runnable through the generic UX
   - `wordlist` modules stay visible but are marked as pending PR 5.7 work
   - Reuse existing job-scoped generic runtime and result browsing semantics from PR 5.5 instead of introducing new storage/progress behavior here

2. **Create target-list generic config view (`internal/tui/views/generic/config.go`)**
   - Input file picker for target lists
   - Extra options input
   - Submit triggers deploy flow with generic Terraform using the already-correct generic worker/runtime path
   - No wordlist-specific controls in this PR

3. **Implement `heph scan --tool <name>` for `target_list` modules**
   - Loads module definition from registry
   - Flags: `--file`, `--options`
   - Current shipped behavior: read existing generic Terraform outputs, then submit → monitor → collect using job-scoped result prefixes from PR 5.5
   - JSON output for piped results
   - `wordlist` modules fail fast with a clear "planned for PR 5.7" error instead of partially working

4. **Prove one actual non-nmap target-list tool end to end**
   - Use `httpx` as the proof case
   - Validate CLI + TUI submission, progress, and result viewing against the generic path
   - This PR is not complete until one non-nmap target-list tool works through the full user surface

5. **Tighten public-facing generic UX messaging**
   - Update README/help text so generic commands describe only shipped target-list capabilities
   - Do not advertise wordlist-driven generic UX until PR 5.7 lands

6. **Add unit tests for dynamic menu and generic config**
   - Generic CLI and TUI coverage should focus on registry-driven UX and parameter handling, not re-testing generic runtime correctness already covered in PR 5.5
   - Add explicit tests that `wordlist` modules are surfaced but rejected or disabled until PR 5.7

### PR 5.7: Wordlist-driven generic UX (`ffuf`, `gobuster`, `feroxbuster`) ✅

**Commits:**

1. **Extend generic config UX for `wordlist` modules**
   - Add wordlist file picker
   - Add target/URL input for modules that require a runtime target separate from the wordlist
   - Add chunk count control and validation
   - Enable runnable menu entries for wordlist-driven modules

2. **Extend `heph scan --tool <name>` for `wordlist` modules**
   - Add `--wordlist`, `--target`, `--chunks`, `--options`
   - Validate required flag combinations based on module shape
   - Keep `target_list` behavior from PR 5.6 unchanged

3. **Add producer-side wordlist submission flow**
   - Split the wordlist into chunk files
   - Upload chunk files to storage
   - Enqueue one generic task per chunk with `InputKey` pointing at the uploaded chunk
   - Carry forward the existing job-scoped result and progress model from PR 5.5

4. **Prove `ffuf` end to end through the generic UX**
   - Full CLI flow
   - Full TUI flow
   - Result viewing over the generic results path

5. **Add tests for wordlist-specific UX and submission**
   - Chunk validation and task generation
   - CLI flag validation for wordlist modules
   - TUI config behavior for wordlist-driven tools
   - Regression coverage that target-list generic tools still follow the simpler PR 5.6 path

The deferred operator UX follow-up previously noted here is promoted into PR 5.10 and PR 5.11 below so lifecycle simplification lands before Phase 6.

### PR 5.8+5.9: Converge nmap onto generic backend and retire dedicated runtime ✅

**What was delivered:**

1. **Collapsed CLI onto generic runtime** — `heph nmap` always emits `worker.Task`, always decodes `worker.Result`, defaults `--terraform-dir` to generic path, uses `nmap-worker` container name. Removed `--use-generic` flag.

2. **Collapsed TUI onto generic runtime** — Nmap config form emits generic deploy config (generic Dockerfile, Terraform, build args). Status view enqueues `worker.Task` with producer-side nmap option injection. Results view decodes `worker.Result`. Container name changed from `nmap-scanner` to `nmap-worker`. Removed nmap-specific env vars (`NMAP_TIMING_TEMPLATE`, `DNS_SERVERS`, `NO_RDNS`) from worker launch.

3. **Deleted dedicated runtime assets** — Removed `cmd/workers/nmap/`, `containers/nmap/Dockerfile`, `deployments/aws/nmap/`. Removed `ConsumerConfig` from `internal/config/`. Removed legacy SQS backward-compatible methods.

4. **Rejected dedicated backend for all tools** — `--backend dedicated` now returns a clear error for any tool. Default backend changed to `generic`.

5. **Updated docs and build targets** — README, CLAUDE.md, ARCHITECTURE.md, TECHDOC.md, PLAN.md updated for single-backend steady state. Makefile no longer builds `nmap-worker` binary or dedicated Docker image. Added migration guidance for operators with existing dedicated infrastructure.

### PR 5.10: Shared operator lifecycle for CLI and TUI

The generic runtime is only "dead easy" if normal operators can treat `heph scan` as the high-level entry point and let the tool decide whether to reuse or deploy infrastructure. `heph infra` should remain available, but it should become the explicit power-user and CI path rather than a prerequisite for routine scans.

**Commits:**

1. **Create a shared lifecycle manager**
   - New shared package that decides whether to reuse existing infrastructure, deploy it, or stop with a clear error
   - Inputs should include requested tool, provider/backend, existing Terraform outputs, and operator policy flags
   - Reused by both CLI and TUI so the same deployment decision logic exists in one place

2. **Make `heph scan --tool <name>` ensure matching infrastructure exists by default**
   - If matching infrastructure for the requested tool already exists, reuse it
   - If infrastructure is missing or was deployed for a different tool, run the generic deploy flow automatically
   - Keep `heph infra deploy` as the explicit operator/power-user command
   - Add lifecycle flags such as `--auto-approve`, `--no-deploy`, and `--destroy-after`

3. **Bring `heph nmap` onto the same lifecycle behavior after backend convergence**
   - Now that `nmap` runs on the generic runtime (PR 5.8-5.9), reuse the same lifecycle manager for `heph nmap`
   - Eliminate the long-term split where `nmap` and generic modules have different deploy assumptions

4. **Teach the TUI to reuse matching infrastructure instead of always replaying deploy**
   - If matching infrastructure already exists, skip directly to submission/status
   - If infrastructure is stale or mismatched, show the reason and offer redeploy through the normal deploy flow
   - Keep the explicit deploy stages visible when actual infra changes are needed

5. **Add lifecycle decision tests**
   - Reuse vs deploy vs block cases
   - Tool mismatch handling
   - `--no-deploy` and `--destroy-after` semantics
   - Regression coverage that power-user `heph infra` flows still work unchanged

### PR 5.11: Operator onboarding, defaults, and status UX

Phase 5 should end with an operator product, not just a correct runtime. That means better diagnostics, saved defaults, resumable status checks, and fewer repeated setup decisions in both the CLI and TUI.

**Commits:**

1. **Implement `heph status`**
   - Add `--job-id` and `--format text|json`
   - Report phase, progress, elapsed time, and result/artifact prefixes using the job-scoped runtime model from PR 5.5
   - Support reattaching to jobs launched from a previous shell session

2. **Create `heph doctor`**
   - Verify AWS credentials/profile, region, Terraform, Docker, and other prerequisites needed for deploy/scan flows
   - Convert common environment and subprocess failures into actionable operator-facing fixes

3. **Add local operator config and `heph init`**
   - Write per-user defaults such as region, profile, worker count, compute mode, cleanup policy, and default output directory
   - Apply the same defaults across CLI and TUI so users stop re-entering routine choices

4. **Upgrade the TUI settings view from passive display to editable defaults + diagnostics**
   - Surface doctor results and prerequisite health checks
   - Allow editing and persisting defaults used by deploy and scan forms
   - Make region/profile/default compute settings visible before a run fails

5. **Add convenience output and cleanup UX**
   - Add `--out <dir>` to download final structured results and artifacts locally after completion
   - Make reuse vs destroy-after behavior explicit in run summaries so cleanup policy is visible instead of implicit
   - This is convenience UX, not a replacement for the fuller export work in Phase 9

6. **Add onboarding and operator UX tests**
   - Config load/save tests
   - Mocked `doctor` checks
   - `status` output and resume coverage
   - Regression coverage that missing prerequisites fail early with clear messages

---

## Phase 6: Selfhosted Provider

The selfhosted provider is the multi-cloud answer for VPS providers and the strongest differentiator vs. Axiom/Ax. Prioritized immediately after Phase 5 because red teamers care about cost and IP diversity above all else. One Go implementation (NATS JetStream queue, MinIO storage, Docker on VMs) covers Hetzner, Linode, Scaleway, Vultr, and any provider with VMs. Adding a new VPS provider is just a Terraform module — the Go code is the same.

Hetzner VPS instances are significantly cheaper than AWS Fargate for scanning workloads, and each VM gets its own public IP (free IP diversity). A single controller VM (~$4.50/mo) runs NATS + MinIO, replacing SQS + S3 entirely.

### PR 6.1: Selfhosted queue and storage providers

**Commits:**

1. **Create `internal/cloud/selfhosted/provider.go`**
   - Implements `cloud.Provider`, `cloud.Storage`, `cloud.Queue`
   - `--cloud selfhosted` CLI flag / TUI provider selector

2. **Create `internal/cloud/selfhosted/nats.go`**
   - Queue implementation via NATS JetStream
   - `Send`/`SendBatch`/`Receive`/`Delete` mapped to NATS publish/subscribe/ack
   - Ack-wait = visibility timeout equivalent, max-deliver = DLQ equivalent

3. **Create `internal/cloud/selfhosted/s3compat.go`**
   - Storage implementation via MinIO (default) or any S3-compatible API
   - Uses the same AWS S3 SDK with custom endpoint (`o.BaseEndpoint`, `o.UsePathStyle = true`)
   - MinIO runs on the controller VM alongside NATS — no external dependencies

4. **Add unit tests with embedded NATS server**

### PR 6.2: Selfhosted compute provider

**Commits:**

1. **Create `internal/cloud/selfhosted/docker.go`**
   - Compute implementation: SSH into provisioned VMs, run Docker containers
   - Worker containers are identical to AWS — same image, different queue/storage endpoints

2. **Create Terraform module for controller VM**
   - `deployments/selfhosted/controller/` — provisions a small VM running NATS JetStream + MinIO
   - Auto-configures MinIO credentials and bucket creation
   - Single VM replaces SQS + S3 + ECR

3. **Add unit tests for compute provider**

### PR 6.3: Hetzner Terraform module

**Commits:**

1. **Create `deployments/hetzner/` Terraform module**
   - Worker VMs (configurable count and type)
   - Controller VM with NATS
   - Controller VM runs NATS + MinIO (no external object storage needed)
   - Networking (private network between workers and controller)
   - Firewall rules must explicitly allow IPv6 egress (Hetzner Cloud Firewalls default-deny)

2. **Add `heph infra deploy --cloud hetzner` support**

3. **Add Terraform validation tests**

4. **Document provider setup in README**

**Note on networking:** Selfhosted VPS providers give IPv6 and IP diversity for free — each VM gets its own public IPv4 + IPv6. No EIGW or multi-NAT needed. See ARCHITECTURE.md "Selfhosted Provider: Networking is Free" section.

---

## Phase 7: Tool Expansion

With the generic module system in place, adding tools is primarily about container images, scale-focused workload handling, and tool-specific result presentation.

### PR 7.1: Per-tool container images

**Commits:**

1. **Build container images for high-priority tools using the generic Dockerfile**
   - Each tool gets a build command in the Makefile:
     - `make docker-build-nuclei` → `docker build --build-arg TOOL_INSTALL_CMD="go install .../nuclei@latest" ...`
     - `make docker-build-subfinder` → similar
     - `make docker-build-httpx` → similar
     - `make docker-build-masscan` → `docker build --build-arg TOOL_INSTALL_CMD="apk add masscan" ...`
   - Tools that need custom Dockerfiles (e.g., tools with complex dependencies) get dedicated files in `containers/<tool>/Dockerfile`

2. **Add container build targets to Makefile and CI**
   - `make docker-build-all` builds all tool images
   - CI builds all images on push

3. **Add smoke tests for container images**
   - Verify each tool binary exists and runs `--help` or `--version`

### PR 7.2: Scale wordlist distribution for large inputs

**Commits:**

1. **Optimize wordlist splitting and upload flow for very large inputs**
   - Preserve the basic wordlist UX and producer flow delivered in PR 5.7
   - Add streaming split/upload behavior tuned for multi-GB and larger wordlists
   - Avoid unnecessary local disk amplification where practical

2. **Add scaling heuristics for wordlist-driven jobs**
   - Auto-size chunk counts from file size and worker count
   - Add guardrails for chunk sizes that would overwhelm container memory
   - Add resumable or restart-friendly behavior where it materially reduces wasted work

3. **Add tests for large-wordlist handling**
   - Even/uneven division
   - Empty files and tiny files
   - Large-file streaming behavior
   - Chunk sizing heuristics

### PR 7.3: TUI result formatting per tool

**Commits:**

1. **Create generic results view (`internal/tui/views/generic/results.go`)**
   - Default: table of S3 result file names with download/view options
   - Scrollable viewport for viewing individual result content

2. **Add tool-specific result formatters**
   - Nmap XML → table of open ports/services
   - Nuclei JSONL → table of findings by severity
   - ffuf JSON → table of hits (URL, status, content length)
   - Default: raw text/JSON display for unrecognized formats

3. **Add unit tests for result formatting**

---

## Phase 8: Naabu + Nmap Pipeline

Uses naabu's `-nmap-cli` flag to run port discovery + deep scanning in one worker pass. Same worker pool model as nmap. No Lambda, no Step Functions.

Key insight: naabu has a built-in `-nmap-cli` flag that pipes discovered open ports directly into nmap within the same process. A single worker can run `naabu -host <target> -nmap-cli 'nmap <flags>'` in one pass, eliminating all multi-phase orchestration. This is critical for Jason Haddix's IPv6 scanning use case at massive scale.

### PR 8.1: Naabu tool package and container

**Commits:**

1. **Create `internal/tools/naabu/` package**
   - `scanner.go` — naabu execution with two modes:
     - Combined (default): `naabu -host {{target}} -nmap-cli 'nmap {{nmap_options}} -oX {{output}}'`
     - Discovery-only: `naabu -host {{target}} -json -o {{output}}`
   - `models.go` — NaabuResult (target + open ports), combined scan output types
   - Target parsing, result handling per mode

2. **Create two YAML module definitions**
   - `internal/modules/definitions/naabu-nmap.yaml` — combined mode (default):
     ```yaml
     name: naabu-nmap
     description: Fast port discovery + deep nmap scan in one pass
     command: "naabu -host {{target}} -nmap-cli 'nmap {{nmap_options}} -oX {{output}}'"
     input_type: target_list
     output_ext: xml
     install_cmd: "apk add --no-cache nmap nmap-scripts && go install github.com/projectdiscovery/naabu/v2/cmd/naabu@latest"
     default_cpu: 512
     default_memory: 1024
     timeout: 15m
     tags: [scanner, network, pipeline]
     ```
   - `internal/modules/definitions/naabu.yaml` — discovery-only:
     ```yaml
     name: naabu
     description: Fast port discovery scanner
     command: "naabu -host {{target}} -json -o {{output}}"
     input_type: target_list
     output_ext: json
     install_cmd: "go install github.com/projectdiscovery/naabu/v2/cmd/naabu@latest"
     default_cpu: 256
     default_memory: 512
     timeout: 5m
     tags: [scanner, network]
     ```

3. **Create container image (`containers/naabu/Dockerfile`)**
   - Installs both naabu and nmap (for combined mode)
   - Alpine base, non-root user

4. **Add unit tests for naabu target parsing and mode selection**

### PR 8.2: Result parsing

**Commits:**

1. **Nmap XML parser for combined mode results**
   - Combined mode outputs nmap XML — reuse existing nmap result parsing

2. **Naabu JSON parser for discovery-only mode**
   - Parse naabu JSON output (target + open ports per line)

3. **Add unit tests for both parsers**

### PR 8.3: TUI/CLI views with mode selector

**Commits:**

1. **Create naabu+nmap config view for TUI**
   - Target file input
   - Mode selector: combined (default) vs discovery-only
   - Nmap options input (for combined mode)
   - Single-phase status display (same as nmap)

2. **Implement `heph naabu` subcommand**
   - `--mode combined|discovery` flag
   - `--nmap-options` flag (for combined mode)

3. **Add unit tests**

---

## Phase 9: Result Export & Pipeline Integration

Red teamers need to pipe results into other tools and reporting systems. This phase makes results first-class pipeline citizens.

### PR 9.1: CLI result export

**Commits:**

1. **Add `heph results` subcommand**
   - `heph results list --job <id>` — list result files from S3
   - `heph results download --job <id> --output <dir>` — download all results to local directory
   - `heph results export --job <id> --format json|csv|jsonl` — export results to stdout in specified format
   - Pipe-friendly: `heph results export --job <id> --format jsonl | jq '.open_ports[]'`

2. **Add `--format json` flag to `heph nmap` and `heph scan`**
   - Machine-readable output for scripting: progress updates as JSON lines, final results as JSON
   - Default remains human-readable text

3. **Add unit tests for export formatting**

### PR 9.2: Result formatters per tool

**Commits:**

1. **Create `internal/results/` package**
   - `Formatter` interface: `Format(data []byte, format string) ([]byte, error)`
   - Nmap XML → JSON/CSV converter
   - Naabu JSON → CSV converter
   - Generic passthrough for unknown formats

2. **Wire formatters into TUI results view and CLI export**

3. **Add unit tests for format conversion**

---

---

## Phase 10: Networking Enhancements

Addresses critical feedback: scans from a single NAT Gateway IP get crushed by SOC/AWS rate-limiting rules.

### PR 10.1: Dual-stack VPC with EIGW (IPv6)

**Commits:**

1. **Modify `deployments/aws/shared/networking/main.tf`**
   - Add `assign_generated_ipv6_cidr_block` on VPC (conditional on `var.enable_ipv6`)
   - Add IPv6 CIDR on subnets, `assign_ipv6_address_on_creation`
   - Create `aws_egress_only_internet_gateway` (conditional on `var.enable_ipv6`)
   - Add `::/0 → EIGW` route for private subnets
   - Add `::/0 → IGW` route for public subnets
   - Add IPv6 egress rule on ECS security group

2. **Add `enable_ipv6` variable, pass through from `environments/dev/`**

3. **Document `dualStackIPv6` ECS account setting prerequisite**

4. **Add Terraform validation and tfsec tests for new resources**

### PR 10.2: Multi-NAT Gateway for IP diversity

**Commits:**

1. **Modify `deployments/aws/shared/networking/main.tf`**
   - `aws_eip.nat` count = `multi_nat ? az_count : 1`
   - `aws_nat_gateway.this` count = `multi_nat ? az_count : 1`, each in its AZ's public subnet
   - Per-AZ `aws_route_table.private` (conditional on `multi_nat`)
   - Route table associations point to correct per-AZ route table

2. **Add `multi_nat` variable, `nat_gateway_public_ips` output**

3. **Add Terraform validation tests**

### PR 10.3: IP diversity configuration in TUI/CLI

**Commits:**

1. **Create `internal/cloud/aws/ec2.go`**
   - Query NAT gateway IPs, EIGW status, dualstack setting

2. **Add networking options to nmap config TUI view**
   - IPv6 toggle, multi-NAT toggle, display current NAT IPs

3. **Add `--enable-ipv6`, `--multi-nat` CLI flags to nmap subcommand**

4. **Add `NetworkingConfig` struct to `internal/config/config.go`**
   - Must be provider-aware: on AWS controls EIGW/multi-NAT toggles, on selfhosted displays per-VM IPs (diversity is inherent)
   - `--enable-ipv6` on AWS triggers EIGW deployment; on selfhosted is a no-op or validates VMs have IPv6 assigned

5. **Add unit tests for networking config**

---

## Future / Deprioritized

The following phases are designed but deferred. Their architecture is documented in `ARCHITECTURE.md` and `WORKLOAD_DISTRIBUTION.md` for reference.

### Hashcat

Hashcat is deprioritized due to:
- GPU kernel variations across instance types need more research (T4 vs A10G vs V100 driver compatibility)
- Wordlist storage on S3 for TB-scale files needs cost analysis
- Cost comparison vs. local 4090 GPU — for many jobs, local cracking may be cheaper
- Spot interruption handling adds significant complexity (sidecar, session save/restore, re-queue)

**When ready, implementation would include:**

- **Hashcat tool package** — `internal/tools/hashcat/`: keyspace calculation (rules-aware), splitting, models
- **Hashcat rules** — `internal/tools/hashcat/rules.go`: rule file validation, S3 upload, `-r` flag construction
- **Hashcat worker** — `cmd/workers/hashcat/main.go`: SQS consumer with `--skip`/`--limit`, rule file download, GPU execution
- **Hashcat sidecar** — `cmd/workers/hashcat/sidecar/main.go`: spot termination detection, session save, re-queue with remaining keyspace
- **Hashcat container** — `containers/hashcat/Dockerfile`: `nvidia/cuda:12.x-runtime-ubuntu22.04` base
- **Hashcat Terraform** — `deployments/aws/hashcat/`: EC2 spot with GPU ASG, launch templates, instance profiles
- **Hashcat TUI/CLI** — config, status, results views; `heph hashcat` subcommand
- **Spot resumption integration test**

See `ARCHITECTURE.md` Hashcat Architecture section and `WORKLOAD_DISTRIBUTION.md` Hashcat Keyspace Distribution section for full design.

### Big 3 Multi-Cloud (GCP, Azure)

Deferred until the AWS implementation is mature and multiple tools are supported. The selfhosted provider (Phase 9) covers VPS providers and is higher priority.

**When ready, implementation would include:**

- **GCP provider** — `internal/cloud/gcp/`: Pub/Sub, GCS, Cloud Run Jobs
- **Azure provider** — `internal/cloud/azure/`: Service Bus, Blob Storage, ACI
- **Per-provider Terraform modules** — `deployments/gcp/`, `deployments/azure/`

See `ARCHITECTURE.md` Cloud Provider Abstraction section and `COMPARISON.md` Multi-Cloud section for full design.

---

## Summary

| Phase | PRs | Focus |
|---|---|---|
| 1. Project restructure | 2 | Directory layout, Makefile, CI |
| 2. TUI + CLI skeleton | 2 | Bubbletea app, CLI subcommands |
| 3. Cloud interfaces + nmap integration | 6 | Cloud interfaces, infra lifecycle, nmap views, EC2 Spot Fleet, CLI, binary rename |
| 4. Nmap enhancements | 4 | Port parsing, port splitting, scan hardening, reverse DNS toggle |
| 5. Generalized module system | 11 | Module definitions, generic runtime, nmap convergence, operator UX closure |
| 6. Selfhosted provider | 3 | NATS JetStream queue, MinIO storage, Hetzner Terraform |
| 7. Tool expansion | 3 | Per-tool containers, wordlist splitting, result formatting |
| 8. Naabu + Nmap pipeline | 3 | Naabu package, combined naabu+nmap worker, TUI/CLI views |
| 9. Result export | 2 | CLI export commands, result formatters per tool |
| 10. Networking enhancements | 3 | Dual-stack VPC, multi-NAT, IP diversity config |
| **Active total** | **39 PRs** | |
| Future: Hashcat | — | Deferred (GPU research, cost analysis needed) |
| Future: Big 3 multi-cloud | — | Deferred (selfhosted provider covers VPS providers first) |
