# Notes: ENG-1889

## § Plan

### Architectural Approach

Agent-first CLI for the Shovels REST API. Go + cobra, single binary. JSON-only output with universal `{data, meta}` envelope. `--limit N` abstracts cursor pagination (default 50, cap 10K, hard ceiling 100K). Meaningful exit codes (0-5). Open-source (MIT), distributed via GoReleaser + Homebrew.

Key decisions from expert convergence (3 rounds):
- Universal envelope `{"data": ..., "meta": {...}}` for ALL commands — no exceptions
- Contractors sub-resources as separate subcommands (not flags on `get`)
- `missing` IDs reported in `meta.missing` (not top-level)
- Cobra SilenceUsage/SilenceErrors + custom SetFlagErrorFunc for JSON error contract
- Retry honors Retry-After header, jittered exponential backoff, --timeout per request
- Exit code 5 for transient server/network errors (distinct from client errors)

### Major Phases (13 steps)

1. **Phase 1: Foundation** (Steps 1-5)
   - E2E test infrastructure, project scaffold, config, HTTP client, output/pagination
   - Outcome: `shovels version` works, full infra proven

2. **Phase 2: Permits** (Steps 6-7)
   - `permits search` (25+ flags) + `permits get` (batch with missing tracking)

3. **Phase 3: Contractors** (Steps 8-10)
   - `contractors search`, `contractors get`, `contractors permits|employees|metrics`
   - Sub-resources as separate subcommands

4. **Phase 4: Remaining** (Steps 11-12)
   - `addresses search`, `usage`, LLM-optimized help text

5. **Phase 5: Distribution** (Step 13)
   - GoReleaser, GitHub Actions, README, Homebrew

### Dependencies & Risks

- **External Dependencies**: Go 1.22+, cobra, viper, GoReleaser
- **Internal Dependencies**: Shovels API v2 (public, stable)
- **Technical Risks**:
  - Flag naming: 25+ query params need CLI-friendly kebab-case names
  - In-memory pagination bounded by 100K ceiling; NDJSON streaming deferred to v2
  - Tag exclusion (`--tags=-roofing`) needs shell escaping documentation

### Out of Scope
- Table/CSV output formatting (JSON-only for v1)
- Geo search/metrics endpoints (v1.1)
- List endpoints (v1.1)
- MCP server mode
- NDJSON streaming (v2)
- Interactive prompts, TUI, telemetry

---

## § Architectural Decisions
<!-- Populated during /strike execution -->

- [Step 5 debt] E2E fixture command setup is now duplicated across three `cmd/test*.go` files; a small shared helper would reduce divergence as more fixture commands are added.
- [Step 6 debt] Flag definitions and help-group membership are duplicated in separate hard-coded lists in [cmd/permits.go](/Users/luka/workspace/shovels-cli/worktrees/ENG-1889/cmd/permits.go), which can drift as filters evolve; a shared flag-metadata registry would reduce divergence risk.
- [Step 7 debt] Client construction, timeout parsing, and API error translation are duplicated between `permits search` and `permits get`; extracting a shared command helper would reduce divergence risk as command count grows.
- [Step 9 debt] `runContractorsGet` duplicates client creation/API error translation/request-building patterns already present in permits get; a shared helper would reduce divergence risk across resource `get` commands.
- [Step 10 debt] [cmd/contractors.go:168](/Users/luka/workspace/shovels-cli/worktrees/ENG-1889/cmd/contractors.go:168), [cmd/contractors.go:214](/Users/luka/workspace/shovels-cli/worktrees/ENG-1889/cmd/contractors.go:214), and [cmd/contractors.go:265](/Users/luka/workspace/shovels-cli/worktrees/ENG-1889/cmd/contractors.go:265) duplicate client setup and API error translation logic; a shared helper would reduce divergence risk and prep-refactor debt.
- [Step 11 debt] Client setup and API error translation logic are duplicated again in new command handlers; a prep refactor to a shared command helper would reduce divergence risk as commands grow.
- [Step 13 debt] CI and release workflows duplicate checkout/setup-go/test setup; a reusable workflow would reduce drift risk between validation paths.
## § Patterns Discovered
<!-- Populated during /strike execution -->

## § Errors
<!-- Populated during /strike execution -->

## § Steps Log
<!-- Populated during /strike execution -->

- Step 1: E2E test infrastructure -> eb7da6e (3 commits)
- Step 2: Project scaffold + root command + version -> 6df9c61 (2 commits)
- Step 3: Config layer -> be3911b (6 commits)
- Step 5: Output envelope + pagination -> 57768bd (7 commits)
- Step 6: `shovels permits search` -> 2e43972 (3 commits)
- Step 7: `shovels permits get` -> 0903824 (2 commits)
- Step 8: `shovels contractors search` -> a82e8b0 (2 commits)
- Step 9: `shovels contractors get` -> 9141a75
- Step 10: `shovels contractors permits|employees|metrics` -> ce11d15 (2 commits)
- Step 11: `shovels addresses search` + `shovels usage` -> f97bc85
- Step 12: LLM-optimized `--help` text -> a7beaeb (2 commits)
- Step 13: GoReleaser + GitHub Actions + README -> 39829b2 (2 commits)
## § Amendments
<!-- For post-strike work discovered during execution -->
