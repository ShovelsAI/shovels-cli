# Spec: ENG-1889 - Shovels CLI — Agent-first REST API CLI (Go)

## EXECUTION PROTOCOL (READ EVERY STEP)

1. **Delegate to BLADE**: `Task(subagent_type="team-spade:blade", prompt="Implement Step N: [title]\n\nSpecs: .spade/ENG-1889/spec.md (Step N section)\nContext: .spade/ENG-1889/notes.md § Architectural Decisions, § Patterns Discovered\n\nWrite e2e tests covering all Behavior scenarios, self-iterate until green.\nCommit when complete (pre-commit hooks validate formatting).")`
2. **After BLADE commits**: Invoke GUARD per state.json guard_mode
   - codex: `${CLAUDE_PLUGIN_ROOT}/scripts/guard.sh "$dir"`
   - subagent: `Task(subagent_type="team-spade:guard", prompt="Review Step N...")`
3. **State transitions are mechanical** — hooks update state.json and notes.md automatically
4. **If BLOCK**: Re-delegate to BLADE with feedback (BLADE iterates freely)
5. **NEVER**: Implement code yourself. Skip GUARD. Write guard-verdict file. Edit state.json during execution.

---

## § Overview

### Goal
Build an agent-first CLI for the Shovels REST API. A single Go binary that any AI agent can use via shell commands with zero configuration beyond an API key. Agents discover commands via `--help`, get JSON output, and parse predictable envelopes.

### Scope
**Included (v1):**
- 8 resource commands: permits search/get, contractors search/get/permits/employees/metrics, addresses search
- 2 utility commands: usage, config set/show
- Universal JSON envelope, `--limit N` pagination, error handling with exit codes
- GoReleaser distribution (Homebrew, go install, GitHub Releases)

**Excluded:**
- Table/CSV output (JSON-only for v1)
- Geo search/metrics endpoints (v1.1)
- List endpoints (v1.1)
- MCP server mode
- NDJSON streaming (v2)
- Interactive prompts, TUI, telemetry

### Success Criteria
- `shovels permits search --geo-id ZIP_90210 --from 2024-01-01 --to 2024-12-31 --tags solar` returns valid JSON envelope
- All commands output `{"data": ..., "meta": {...}}` consistently
- `--limit all` respects 10,000 default cap
- Exit codes correctly distinguish auth (2), rate-limit (3), credits (4), transient (5) errors
- Binary builds for macOS/Linux/Windows via GoReleaser

---

## § Research Findings

### Validated Assumptions
- **cobra + viper**: Industry standard for Go CLIs (gh, kubectl, stripe). Well-documented, actively maintained.
- **GoReleaser**: Standard release tooling. Tag-based releases, Homebrew tap generation, checksums.
- **Agent CLI ergonomics**: LLMs know CLI patterns from pre-training. `--help`, JSON output, exit codes are universal.

### Known Limitations
- **cobra default behavior**: Prints plain-text errors and usage to stderr. Must explicitly set SilenceUsage, SilenceErrors, and custom SetFlagErrorFunc to maintain JSON error contract.
- **viper env binding**: Environment variable names auto-derived from flag names can conflict. Must bind explicitly.
- **In-memory pagination**: `--limit all` assembles results in memory. Bounded by hard ceiling (100K v1). NDJSON streaming deferred to v2.

### Disallowed Approaches (apply to all steps)
- **Plain-text errors to stderr**: All error output must be JSON. No cobra default error printing.
- **Interactive prompts**: Never prompt, never block. All input via flags/env/config.
- **Unbounded pagination**: `--limit` and `--max-records` both have hard ceilings (10K default, 100K max).

---

## § Implementation Steps

> **Behavior-Driven Requirements**:
> - Each step defines BEHAVIOR with 4 mandatory categories
> - Spec defines the FLOOR — BLADE can exceed but not go below
> - BLADE writes e2e tests from Behaviors, self-iterate until green
> - BLADE has implementation freedom within constraints
> - No code in specs (except example I/O)

---

### Step 1: E2E test infrastructure

#### Intent
Establish the test harness that all subsequent steps use to verify CLI behavior end-to-end. Building the binary and invoking it as a subprocess proves the actual user experience.

#### Behavior

**Happy paths:**
- GIVEN binary is built and `SHOVELS_API_KEY` is set WHEN `./shovels version` is executed via exec.Command THEN stdout is valid JSON with `data.version` field and process exits 0

**Edge cases:**
- GIVEN `SHOVELS_API_KEY` is not set WHEN e2e tests run THEN tests skip with `t.Skip("SHOVELS_API_KEY not set")`

**Error conditions:**
- GIVEN binary fails to compile WHEN `TestMain` runs THEN test suite fails immediately with build error (unit)

**Boundary conditions:**
- GIVEN a command writes to both stdout and stderr WHEN executed THEN e2e harness captures them independently for separate assertions (unit)

#### E2E Testing
Environment: CLAUDE.md § Testing
Command: `go test -tags=e2e ./e2e/...`

#### Constraints
- Binary built once in TestMain, reused across all tests
- Tests must be gated by `SHOVELS_API_KEY` presence — CI can skip if no key
- stdout and stderr captured separately (not interleaved)

#### Files
- `e2e/helpers_test.go` — TestMain, binary build, exec helper
- `e2e/version_test.go` — Smoke test for version command
- `CLAUDE.md` — Add E2E row to Testing table

#### Implementation
Implementer determines approach. Document in commit.

---

### Step 2: Project scaffold + root command + version

#### Intent
Establish the Go module, directory structure, cobra root command with global flags, and the version command. This is the skeleton everything else hangs on.

#### Behavior

**Happy paths:**
- GIVEN binary is built WHEN `shovels --help` is executed THEN stdout shows available commands and global flags (--api-key, --limit, --max-records, --base-url, --no-retry, --timeout), exit 0
- GIVEN binary is built WHEN `shovels version` is executed THEN stdout is `{"data": {"version": "...", "commit": "...", "date": "..."}, "meta": {}}`, exit 0

**Edge cases:**
- GIVEN no arguments WHEN `shovels` is executed THEN shows help text (same as --help), exit 0
- GIVEN unknown command WHEN `shovels foobar` is executed THEN stderr JSON error, exit 1

**Error conditions:**
- GIVEN unknown flag WHEN `shovels --unknown-flag` is executed THEN stderr JSON error (not plain text), exit 1

**Boundary conditions:**
- GIVEN `--api-key` global flag WHEN provided on any command THEN accessible to all subcommands via cobra persistent flags (unit)

#### Example I/O

**`shovels version`:**
```json
{"data": {"version": "0.1.0", "commit": "abc1234", "date": "2026-02-27T00:00:00Z"}, "meta": {}}
```

**`shovels --unknown` (stderr):**
```json
{"error": "unknown flag: --unknown", "code": 1}
```

#### E2E Testing
Environment: CLAUDE.md § Testing
Command: `go test -tags=e2e ./e2e/...`

#### Constraints
- [Reviewable] SilenceUsage: true, SilenceErrors: true on root command
- [Reviewable] Custom SetFlagErrorFunc that writes JSON to stderr
- [Reviewable] Centralized JSON error writer used by ALL error paths (flag errors, unknown commands, etc.)
- [Reviewable] Version info injected via ldflags at build time
- `--help` output is plain text (sole exception to JSON-only)

#### Permitted Approaches
- cobra-cli scaffolding or manual cobra setup
- ldflags for version injection: `-X main.version=...`

#### Disallowed Approaches
- Default cobra error handling (prints plain text)
- Hardcoded version strings

#### Files
- `main.go` — Entry point
- `cmd/root.go` — Root command, global persistent flags, error handling
- `cmd/version.go` — Version command
- `internal/output/output.go` — JSON envelope writer, JSON error writer
- `go.mod` — Module definition

#### Implementation
Implementer determines approach. Document in commit.

---

### Step 3: Config layer

#### Intent
Users configure their API key once and it persists across sessions. The precedence chain (flag > env > file) follows industry conventions agents already know.

#### Behavior

**Happy paths:**
- GIVEN `~/.config/shovels/config.yaml` contains `api_key: sk-test` WHEN any API command runs THEN that key is used for `X-API-Key` header
- GIVEN `SHOVELS_API_KEY=sk-env` is set WHEN any command runs THEN env var overrides config file
- GIVEN `--api-key sk-flag` is passed WHEN any command runs THEN flag overrides env and config
- GIVEN `shovels config set api-key sk-new` WHEN run THEN `~/.config/shovels/config.yaml` is created/updated with api_key
- GIVEN `shovels config show` WHEN run THEN stdout is `{"data": {"api_key": "sk-***masked", "base_url": "...", "default_limit": 50}, "meta": {}}`

**Edge cases:**
- GIVEN config file doesn't exist WHEN `shovels config set api-key sk-x` is run THEN config directory and file are created
- GIVEN config file has other keys WHEN `shovels config set api-key sk-x` is run THEN only api_key updated, others preserved

**Error conditions:**
- GIVEN no API key from any source WHEN an API command runs THEN stderr JSON: `"API key not configured. Set SHOVELS_API_KEY or run: shovels config set api-key <key>"`, exit 2
- GIVEN config directory not writable WHEN `shovels config set` runs THEN stderr error, exit 1

**Boundary conditions:**
- GIVEN `--base-url` flag WHEN provided THEN overrides default `https://api.shovels.ai/v2` (unit)

#### E2E Testing
Environment: CLAUDE.md § Testing
Command: `go test -tags=e2e ./e2e/...`

#### Constraints
- Config path follows XDG convention: `~/.config/shovels/config.yaml`
- API key masked in `config show` output (show first 4 + last 4 chars only)
- Config resolution must happen in a cobra PersistentPreRun so all subcommands inherit it

#### Files
- `cmd/config.go` — Config set/show commands
- `internal/config/config.go` — Config resolution (flag > env > file > default)

#### Implementation
Implementer determines approach. Document in commit.

---

### Step 4: HTTP client + error handling

#### Intent
A shared HTTP client that all commands use. Handles authentication, credit tracking, retries, timeouts, and translates API errors into structured JSON stderr output with correct exit codes.

#### Behavior

**Happy paths:**
- GIVEN valid API key WHEN request sent THEN includes `X-API-Key` header and `User-Agent: shovels-cli/<version>`
- GIVEN API returns 200 WHEN response parsed THEN credit headers extracted (`X-Credits-Request`, `X-Credits-Limit`, `X-Credits-Remaining`)

**Edge cases:**
- GIVEN API returns 200 with no credit limit headers (unlimited plan) WHEN parsed THEN `credits_remaining` is null in meta
- GIVEN API returns 200 with empty items WHEN parsed THEN data is empty array, count is 0

**Error conditions:**
- GIVEN API returns 401 THEN stderr JSON error, exit 2
- GIVEN API returns 402 THEN stderr JSON "Credit limit exceeded", exit 4
- GIVEN API returns 422 THEN stderr JSON with field-level validation errors from API, exit 1
- GIVEN API returns 429 THEN client retries with jittered exponential backoff (1s, 2s, 4s ±25%), honoring Retry-After header when present, max 3 retries
- GIVEN API returns 429 AND `--no-retry` is set THEN no retry, exit 3
- GIVEN API returns 5xx THEN stderr JSON "Server error", exit 5
- GIVEN network error (timeout, DNS failure) THEN stderr JSON with details, exit 5

**Boundary conditions:**
- GIVEN 429 retry succeeds on 2nd attempt THEN only successful response is returned (no partial output from failed attempts) (unit)
- GIVEN 429 retries exhaust max (3) THEN stderr JSON "Rate limited after 3 retries", exit 3
- GIVEN Retry-After header with value "5" THEN use 5s as delay instead of computed backoff (unit)
- GIVEN --timeout 10s WHEN request takes 15s THEN context cancelled, stderr JSON timeout error, exit 5

#### E2E Testing
Environment: CLAUDE.md § Testing
Command: `go test -tags=e2e ./e2e/...`

#### Constraints
- [Reviewable] All HTTP calls go through internal/client — commands never call net/http directly
- [Reviewable] Error responses include `error_type` field for machine classification (e.g., "auth_error", "rate_limited", "credit_exhausted", "validation_error", "server_error", "network_error")
- [Runtime] Per-request context timeout via --timeout flag (default 30s)

#### Files
- `internal/client/client.go` — HTTP client, auth, retry logic, credit extraction
- `internal/client/errors.go` — Error types, exit code mapping

#### Implementation
Implementer determines approach. Document in commit.

---

### Step 5: Output envelope + pagination

#### Intent
Abstract cursor-based pagination behind `--limit N` so agents never deal with cursors. Provide a universal `{data, meta}` envelope so every command's output is structurally predictable.

#### Behavior

**Happy paths:**
- GIVEN `--limit 50` (default) WHEN search command runs THEN single API page fetched, stdout: `{"data": [...], "meta": {"count": 50, "has_more": true, "credits_used": 50, "credits_remaining": 9950}}`
- GIVEN `--limit 200` WHEN search runs with 200+ results THEN CLI fetches 4 pages internally (size=50 each), assembles result, `"count": 200, "has_more": true`
- GIVEN `--limit all` WHEN search runs THEN CLI follows cursors up to 10,000 record cap, `"has_more"` reflects whether more exist beyond cap

**Edge cases:**
- GIVEN `--limit 200` but only 75 results exist THEN CLI fetches 2 pages (50+25), `"count": 75, "has_more": false`
- GIVEN `--limit 1` THEN CLI sends `size=1`, returns single item in data array
- GIVEN non-paginated command (e.g., `usage`) WHEN run THEN `{"data": {...}, "meta": {"credits_used": N, "credits_remaining": N}}` — no count/has_more

**Error conditions:**
- GIVEN `--limit -1` or `--limit 0` THEN stderr validation error, exit 1
- GIVEN `--limit abc` (non-numeric, not "all") THEN stderr validation error, exit 1
- GIVEN `--limit 200000` (exceeds 100K ceiling) THEN stderr error "limit cannot exceed 100000", exit 1
- GIVEN `--max-records 200000` (exceeds 100K ceiling) THEN stderr error "max-records cannot exceed 100000", exit 1
- GIVEN mid-pagination API error (page 3 of 5 fails) THEN stderr error, no partial output, exit code matches error type

**Boundary conditions:**
- GIVEN `--limit 75` THEN first request sends size=50, second sends size=25 (unit)
- GIVEN `--limit all` with 15,000 results THEN fetches up to 10,000 (default cap), `"has_more": true`, `"count": 10000`
- GIVEN `--limit all --max-records 50000` THEN fetches up to 50,000 records
- GIVEN `meta.count` THEN always equals number of records actually returned in `data` array (not requested)

#### Example I/O

**Paginated:**
```json
{"data": [{...}, {...}], "meta": {"count": 50, "has_more": true, "credits_used": 50, "credits_remaining": 9950}}
```

**Non-paginated (usage):**
```json
{"data": {"credits_used": 5432, "credit_limit": 10000, "is_over_limit": false}, "meta": {"credits_used": 0, "credits_remaining": 10000}}
```

**Version (no API call):**
```json
{"data": {"version": "0.1.0"}, "meta": {}}
```

#### E2E Testing
Environment: CLAUDE.md § Testing
Command: `go test -tags=e2e ./e2e/...`

#### Constraints
- [Reviewable] 100,000 hard ceiling enforced on ALL code paths (both `--limit N` and `--max-records N`)
- [Reviewable] No partial output on mid-pagination errors — either full result or full error
- Results assembled in memory (v1). NDJSON streaming deferred to v2.

#### Validation Gates
- [ ] `--limit all` with default cap correctly stops at 10,000 and reports has_more: true when more exist

#### Files
- `internal/output/envelope.go` — Universal envelope builder
- `internal/client/paginator.go` — Cursor loop, limit/size calculation, cap enforcement

#### Implementation
Implementer determines approach. Document in commit.

---

### Step 6: `shovels permits search`

#### Intent
The primary search command. Agents find building permits by location, date range, type, and dozens of filter criteria.

#### Behavior

**Happy paths:**
- GIVEN `shovels permits search --geo-id ZIP_90210 --from 2024-01-01 --to 2024-12-31 --tags solar` THEN stdout JSON envelope with permit data, exit 0
- GIVEN multiple tags `--tags solar --tags roofing` THEN both sent to API (AND logic)
- GIVEN exclusion tag `--tags solar --tags=-roofing` THEN `-roofing` passed to API as-is

**Edge cases:**
- GIVEN no results match filters THEN `{"data": [], "meta": {"count": 0, "has_more": false, ...}}`, exit 0
- GIVEN all optional flags omitted THEN valid request with only required geo/date filters

**Error conditions:**
- GIVEN missing `--from`, `--to`, or `--geo-id` THEN stderr error listing required flags, exit 1
- GIVEN invalid date format `--from 2024/01/01` THEN stderr validation error, exit 1

**Boundary conditions:**
- GIVEN `--query` with 51+ chars THEN stderr validation error (API max 50), exit 1
- GIVEN `--status` with invalid value THEN stderr error listing valid options (final, in_review, inactive, active), exit 1

#### Example I/O

**Request:**
```bash
shovels permits search --geo-id ZIP_90210 --from 2024-01-01 --to 2024-12-31 --tags solar --limit 10
```

**Response:**
```json
{"data": [{"id": "P_abc123", "description": "Solar panel installation", "status": "final", ...}], "meta": {"count": 10, "has_more": true, "credits_used": 10, "credits_remaining": 9990}}
```

#### E2E Testing
Environment: CLAUDE.md § Testing
Command: `go test -tags=e2e ./e2e/...`

#### Constraints
- Required flags: `--from` (YYYY-MM-DD), `--to` (YYYY-MM-DD), `--geo-id`
- Flag help text grouped by category: required, permit filters, property filters, contractor filters
- All ~25 optional flags mapped from API query params with kebab-case naming

#### Files
- `cmd/permits.go` — Permits parent command + search/get subcommands
- `e2e/permits_test.go` — E2E tests for permits commands

#### Implementation
Implementer determines approach. Document in commit.

---

### Step 7: `shovels permits get`

#### Intent
Fetch specific permits by ID. Agents use this after discovering permit IDs from search results.

#### Behavior

**Happy paths:**
- GIVEN `shovels permits get P123 P456` THEN stdout JSON envelope with matching permits in data array, exit 0

**Edge cases:**
- GIVEN single ID `shovels permits get P123` THEN same envelope format (data array with 1 element)
- GIVEN some IDs don't exist THEN found permits in `data`, missing IDs in `meta.missing`

**Error conditions:**
- GIVEN no IDs `shovels permits get` THEN stderr "at least one permit ID required", exit 1
- GIVEN 51+ IDs THEN stderr "maximum 50 IDs per request", exit 1

**Boundary conditions:**
- GIVEN exactly 50 IDs THEN valid single request

#### Example I/O

```json
{"data": [{"id": "P123", ...}], "meta": {"missing": ["P999"], "count": 1, "credits_used": 1, "credits_remaining": 9999}}
```

#### E2E Testing
Environment: CLAUDE.md § Testing
Command: `go test -tags=e2e ./e2e/...`

#### Constraints
- IDs as positional args (not flags)
- `meta.missing` only present when IDs are not found; omitted when all IDs found

#### Files
- `cmd/permits.go` — Get subcommand added to permits parent

#### Implementation
Implementer determines approach. Document in commit.

---

### Step 8: `shovels contractors search`

#### Intent
Search for contractors by location, work type, and performance metrics. Same filter pattern as permits search.

#### Behavior

**Happy paths:**
- GIVEN `shovels contractors search --geo-id ZIP_90210 --from 2024-01-01 --to 2024-12-31` THEN stdout JSON envelope with contractor data, exit 0
- GIVEN `--contractor-classification general_building` THEN filters by derived classification

**Edge cases:**
- GIVEN `--no-tallies` flag THEN `include_tallies=false` sent to API
- GIVEN no results THEN `{"data": [], "meta": {"count": 0, "has_more": false, ...}}`, exit 0

**Error conditions:**
- GIVEN missing required `--from`, `--to`, or `--geo-id` THEN stderr error, exit 1

**Boundary conditions:**
- Same date/geo/min-* validation rules as permits search

#### E2E Testing
Environment: CLAUDE.md § Testing
Command: `go test -tags=e2e ./e2e/...`

#### Constraints
- Shares most flag definitions with permits search (DRY via shared flag registration helper)
- `--no-tallies` is contractors-specific

#### Files
- `cmd/contractors.go` — Contractors parent + search subcommand

#### Implementation
Implementer determines approach. Document in commit.

---

### Step 9: `shovels contractors get`

#### Intent
Fetch specific contractors by ID or in batch. Clean separation from sub-resource commands.

#### Behavior

**Happy paths:**
- GIVEN `shovels contractors get ABC123` THEN stdout JSON envelope with single contractor in data object, exit 0
- GIVEN `shovels contractors get ABC123 DEF456` THEN batch get, data array with contractors

**Edge cases:**
- GIVEN some IDs don't exist in batch THEN found in `data`, missing in `meta.missing`

**Error conditions:**
- GIVEN no IDs THEN stderr error, exit 1
- GIVEN 51+ IDs THEN stderr error, exit 1

**Boundary conditions:**
- GIVEN single ID THEN `data` is object (not array). GIVEN multiple IDs THEN `data` is array.

#### E2E Testing
Environment: CLAUDE.md § Testing
Command: `go test -tags=e2e ./e2e/...`

#### Files
- `cmd/contractors.go` — Get subcommand

#### Implementation
Implementer determines approach. Document in commit.

---

### Step 10: `shovels contractors permits|employees|metrics`

#### Intent
Access sub-resources of a specific contractor. Each is a separate subcommand with clear single-purpose semantics — no ambiguity about response shape.

#### Behavior

**Happy paths:**
- GIVEN `shovels contractors permits ABC123` THEN stdout JSON envelope with contractor's permits, paginated
- GIVEN `shovels contractors employees ABC123` THEN stdout JSON envelope with contractor's employees, paginated
- GIVEN `shovels contractors metrics ABC123 --metric-from 2024-01-01 --metric-to 2024-12-31 --property-type residential --tag solar` THEN stdout JSON envelope with monthly metrics

**Edge cases:**
- GIVEN no permits for contractor THEN `{"data": [], "meta": {"count": 0, "has_more": false, ...}}`, exit 0

**Error conditions:**
- GIVEN `shovels contractors metrics ABC123` without required flags THEN stderr error listing required: --metric-from, --metric-to, --property-type, --tag, exit 1
- GIVEN no ID provided THEN stderr error, exit 1

**Boundary conditions:**
- Each subcommand accepts exactly one positional ID argument

#### E2E Testing
Environment: CLAUDE.md § Testing
Command: `go test -tags=e2e ./e2e/...`

#### Constraints
- metrics requires: --metric-from, --metric-to, --property-type, --tag (all required)
- permits and employees are paginated, support --limit

#### Files
- `cmd/contractors.go` — Add permits, employees, metrics subcommands
- `e2e/contractors_test.go` — E2E tests for all contractor commands

#### Implementation
Implementer determines approach. Document in commit.

---

### Step 11: `shovels addresses search` + `shovels usage`

#### Intent
Complete the v1 command set with address search and credit usage checking.

#### Behavior

**Happy paths:**
- GIVEN `shovels addresses search --query "123 Main St"` THEN stdout JSON envelope with matching addresses, exit 0
- GIVEN `shovels addresses search -q "San Francisco"` THEN same (short flag)
- GIVEN `shovels usage` THEN stdout `{"data": {"credits_used": N, "credit_limit": N, ...}, "meta": {...}}`, exit 0

**Edge cases:**
- GIVEN no matching addresses THEN `{"data": [], "meta": {"count": 0, "has_more": false, ...}}`, exit 0
- GIVEN unlimited plan THEN `credit_limit` is null in usage response

**Error conditions:**
- GIVEN `shovels addresses search` without `--query` THEN stderr "query is required", exit 1

**Boundary conditions:**
- Usage is non-paginated — `meta` has credits but no count/has_more

#### E2E Testing
Environment: CLAUDE.md § Testing
Command: `go test -tags=e2e ./e2e/...`

#### Files
- `cmd/addresses.go` — Addresses parent + search subcommand
- `cmd/usage.go` — Usage command
- `e2e/addresses_test.go` — E2E tests
- `e2e/usage_test.go` — E2E tests

#### Implementation
Implementer determines approach. Document in commit.

---

### Step 12: LLM-optimized `--help` text

#### Intent
Make every command immediately usable by AI agents. Help text is the primary documentation — it must be specific, example-rich, and structured for machine parsing.

#### Behavior

**Happy paths:**
- GIVEN `shovels --help` THEN shows one-line description, all commands, global flags with defaults
- GIVEN `shovels permits search --help` THEN shows: description, required flags marked "(required)", all optional flags with types, example values, grouped by category
- GIVEN `shovels permits --help` THEN lists available subcommands: search, get

**Edge cases:**
- All `--help` output is plain text (sole exception to JSON-only rule)

**Error conditions:**
- (--help always exits 0)

**Boundary conditions:**
- Descriptions use concrete language: "Search building permits by location, date range, permit type, and contractor" not "Search permits with advanced filtering"
- Flag descriptions include value hints: `--from DATE  Start date in YYYY-MM-DD format (required)`

#### E2E Testing
Environment: CLAUDE.md § Testing
Command: `go test -tags=e2e ./e2e/...`

#### Constraints
- [Reviewable] No generic descriptions like "advanced filtering" or "various options"
- [Reviewable] Required flags clearly marked
- [Reviewable] Flags grouped by category in help output (required, permit filters, property filters, contractor filters)

#### Files
- All `cmd/*.go` files — Update Use, Short, Long, and flag descriptions
- `e2e/help_test.go` — Verify help text contains expected content

#### Implementation
Implementer determines approach. Document in commit.

---

### Step 13: GoReleaser + GitHub Actions + README

#### Intent
Make the CLI installable by clients across all major platforms. One `brew install` or `curl` command and they're ready.

#### Behavior

**Happy paths:**
- GIVEN git tag `v0.1.0` is pushed WHEN GitHub Actions runs THEN GoReleaser builds binaries for macOS (amd64/arm64), Linux (amd64/arm64), Windows (amd64)
- GIVEN build succeeds THEN GitHub Release created with binaries, SHA256 checksums, changelog
- GIVEN README THEN contains install instructions (Homebrew, go install, curl), quick start, command reference

**Edge cases:**
- GIVEN archive naming THEN `shovels_{version}_{os}_{arch}.tar.gz` (`.zip` for Windows)
- GIVEN Homebrew formula THEN auto-generated by GoReleaser pointing to release assets

**Error conditions:**
- GIVEN build fails on any platform THEN CI fails, no release created

**Boundary conditions:**
- GIVEN `go install github.com/shovels-ai/shovels-cli@latest` THEN module path matches repo path exactly

#### E2E Testing
Omitted: Distribution/CI configuration step — verified by successful release pipeline execution.

#### Constraints
- CI must run `go test ./...` (unit) before release
- GoReleaser config at repo root: `.goreleaser.yaml`

#### Files
- `.goreleaser.yaml` — GoReleaser configuration
- `.github/workflows/release.yml` — Tag-triggered release workflow
- `.github/workflows/ci.yml` — PR/push test workflow
- `README.md` — Install, quick start, command reference

#### Implementation
Implementer determines approach. Document in commit.

---

## § Technical Considerations

- **Architecture**: cobra command tree in `cmd/`, shared HTTP client in `internal/client/`, config in `internal/config/`, output formatting in `internal/output/`
- **Dependencies**: cobra, viper, Go 1.22+ (for range-over-func if needed), GoReleaser
- **Edge Cases**: Tag exclusion with `-` prefix needs careful shell escaping; `--tags=-roofing` may need `--tags="-roofing"` in some shells
- **Universal Envelope**: `{data, meta}` is the contract. `data` type varies (object/array). `meta` keys are additive per context. No top-level keys besides `data` and `meta`.

---

## § Amendments

*(Empty - for mid-implementation design changes)*

**Amendment steps:** Continue numeric sequence (Step N+1, N+2...) so the state machine tracks them mechanically. Mark as amendments in the step title (e.g., "### Step 14: [Amendment] Fix retry timeout").
