# Heph4estus Roadmap

10 active phases, 39 PRs. Hashcat is deferred. See `IMPLEMENTATION.md` for detailed PR and commit breakdowns.

## Phase Summary

| Phase | Name | PRs | Status |
|-------|------|-----|--------|
| 1 | Project Restructure | 2 | DONE |
| 2 | TUI + CLI Skeleton | 2 | DONE |
| 3 | Cloud Interfaces + Nmap Integration | 6 | DONE |
| 4 | Nmap Enhancements | 4 | DONE |
| 5 | Generalized Module System | 11 | In Progress (7/11 PRs done) |
| 6 | Selfhosted Provider | 3 | Pending |
| 7 | Tool Expansion | 3 | Pending |
| 8 | Naabu + Nmap Pipeline | 3 | Pending |
| 9 | Result Export & Pipeline Integration | 2 | Pending |
| 10 | Networking Enhancements | 3 | Pending |
| Future | Hashcat | — | Deprioritized |
| Future | Big 3 Multi-Cloud (GCP, Azure) | — | Deprioritized |

## Current Focus

**Phase 5: Generalized Module System** — all modules (including nmap) run on the generic backend; remaining work is operator lifecycle closure before Phase 6 (PR 5.10-5.11).

### Phase 5 (in progress)

- PR 5.1: Module definition format and registry ✅
- PR 5.2: Generic worker binary and container ✅
- PR 5.3: Generic Terraform module ✅
- PR 5.4: Nmap migration to generic system ✅
- PR 5.5: Generic execution correctness and hardening ✅
- PR 5.6: Wire target-list generic modules into TUI and CLI ✅
- PR 5.7: Wordlist-driven generic UX (`ffuf`, `gobuster`, `feroxbuster`) ✅
- PR 5.8: Make the generic backend the default runtime for `nmap` ✅
- PR 5.9: Remove the dedicated nmap backend after parity is proven ✅
- PR 5.10: Shared operator lifecycle for CLI and TUI
- PR 5.11: Operator onboarding, defaults, and status UX

### Phase 4 (completed)

- PR 4.1: Port parsing library ✅
- PR 4.2: Integrate port splitting into scanner and producer ✅
- PR 4.3: Scan hardening (retry logic, jitter, optional `--dns-servers` passthrough) ✅
- PR 4.4: Disable reverse DNS (`--no-rdns` flag) ✅

### Phase 3 (completed)

- PR 3.1: Cloud provider interfaces and AWS implementation ✅
- PR 3.2: Infrastructure lifecycle package ✅
- PR 3.3: Nmap TUI views (worker pool architecture) ✅
- PR 3.4: Nmap CLI subcommand ✅
- PR 3.5: Consumer refactor ✅ (absorbed into PR 3.3)
- PR 3.6: EC2 Spot Fleet support ✅
- PR 3.7: Rename CLI binary to `heph` ✅

## Dependency Graph

```
Phase 1 (DONE) → Phase 2 (DONE) → Phase 3 (DONE) → Phase 4 (DONE) → Phase 5 (ACTIVE) → Phase 6
                                                                                        |
                                                           +--------+--------+-----------+--------+
                                                           |        |        |                    |
                                                        Phase 7  Phase 8  Phase 9             Phase 10
                                                       (tools)  (naabu)  (export)            (network)
```

- Phases 1–6 are strictly sequential (each builds on the previous)
- Phases 7–10 can be worked on in parallel after Phase 6
- Phase 6 (selfhosted provider) is prioritized immediately after Phase 5 — red teamers care about cost and IP diversity above all else, and VPS support is the strongest differentiator vs. Axiom/Ax
- Phase 5 now explicitly includes operator lifecycle closure before Phase 6 so provider expansion does not duplicate AWS-specific deploy friction and missing onboarding work
- Phase 7 (tool expansion) requires the generic module system from Phase 5
- Phase 8 (naabu pipeline) uses generic module definitions — a YAML module, no custom orchestration
- Phase 9 (result export) useful immediately, benefits from generic modules
- Phase 10 (networking) is independent AWS infrastructure work

## Key Decisions

- **Generic module system before tool-specific implementations** — ~90% of security tools follow the same "input → run → output" pattern. Building the generic system first (Phase 5) means adding tools is a YAML definition + Dockerfile, not 3-5 PRs each
- **`nmap` should converge to the generic backend** — `nmap` still needs specialized planning and UX (port splitting, timing/DNS/rDNS controls), but those are planner/presentation concerns. Long-term it should run on the same generic deploy/worker/result backend as other target-list tools, with the dedicated backend treated as transitional only
- **Operator lifecycle closure belongs in Phase 5** — `heph scan` should become the high-level operator entry point that ensures matching infrastructure exists, reuses it when safe, and hides backend/deploy complexity by default. `heph infra` remains the explicit power-user command
- **Naabu uses `-nmap-cli` flag** — Single-phase combined scanning, no Lambda bridge or Step Functions. Naabu is a generic module definition, not custom infrastructure
- **Selfhosted provider immediately after module system (Phase 6)** — One `selfhosted` provider (NATS JetStream + MinIO + Docker on VMs) covers any VPS provider with a Terraform module. MinIO runs on the controller VM alongside NATS — zero external dependencies, works on any VPS. This is the killer feature for adoption — cheap VPS instances with IP diversity per VM. GCP/Azure deferred to Future
- **Result export early** — Red teamers need to pipe results into reporting tools and other scanners. `--format json` output and export commands are table stakes
- **CLI binary renamed to `heph`** — Short, easy to type, memorable. `heph nmap`, `heph scan --tool nuclei`, `heph infra deploy`
- **Scan hardening before networking** — Retry logic, jitter, and DNS options must be in place before adding IP diversity (otherwise scans are loud and fragile)
- **Hashcat deprioritized** — GPU kernel variations, wordlist storage costs, and cost analysis vs. local GPU need more research. Design preserved in ARCHITECTURE.md
