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

## § Patterns Discovered
<!-- Populated during /strike execution -->

## § Errors
<!-- Populated during /strike execution -->

## § Steps Log
<!-- Populated during /strike execution -->

- Step 1: E2E test infrastructure -> eb7da6e (3 commits)
- Step 2: Project scaffold + root command + version -> 6df9c61 (2 commits)
- Step 3: Config layer -> be3911b (6 commits)
## § Amendments
<!-- For post-strike work discovered during execution -->
