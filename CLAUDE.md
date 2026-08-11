# Shovels CLI — Engineering Standards

@~/.claude/plugins/marketplaces/shovels-claude-plugins/engineering/CLAUDE.md

> Agent-first CLI for the Shovels REST API. Go + cobra.

---

## Project Overview

- **Language:** Go
- **CLI framework:** [cobra](https://github.com/spf13/cobra)
- **Config:** [viper](https://github.com/spf13/viper)
- **Build/release:** GoReleaser + GitHub Actions
- **License:** MIT
- **API spec:** https://api.shovels.ai/v2/openapi.json

## Architecture

```
cmd/            cobra command tree (one file per resource)
  root.go       root command, global flags
  permits.go    permits search / get
  contractors.go
  addresses.go
  cities.go     cities search (geo_id resolution)
  counties.go   counties search (geo_id resolution)
  jurisdictions.go  jurisdictions search (geo_id resolution)
  tags.go       tags list (tag discovery)
  usage.go
  config.go
  version.go
evals/          LLM usability evals (build tag: eval)
e2e/            stubbed end-to-end tests (build tag: e2e)
integration/    live contract tests (build tag: integration)
internal/
  client/       HTTP client (generated types, hand-crafted calls)
  config/       config file + env var resolution
  output/       JSON output formatting, error rendering
```

## Design Principles

- **JSON-only output.** No tables, no colors. Every response is valid JSON to stdout.
- **Errors to stderr.** Structured JSON errors go to stderr so stdout is always parseable.
- **`--limit N` bounds the record set a command collects.** The CLI handles cursor mechanics internally. Default: 50. `--limit all` fetches up to `--max-records`. A command that collects no such set rejects the flag.
- **The address, city, county and jurisdiction searches are capped by the endpoint.** 20 results for addresses, 15 for the other three, no cursor on any, so `--limit` only lowers those counts.
- **Never interactive.** No prompts, no spinners, no progress bars. Fail loudly with clear messages.
- **Meaningful exit codes:** 0=success, 1=client error, 2=auth error, 3=rate-limit, 4=credits-exhausted, 5=transient/server error.
- **`--help` text is for LLMs.** Write descriptions as if an AI agent is reading them — specific, example-rich, no jargon.

## Auth Precedence

`SHOVELS_API_KEY` env var > `~/.config/shovels/config.yaml`

## Testing

| Layer | Command | Notes |
|-------|---------|-------|
| Unit | `go test ./...` | No network calls, no API key. Mock HTTP client; the schema generator guard reads the pinned `cmd/testdata/openapi.json` |
| Unit, fixtures compiled | `go test -tags=e2e ./cmd/...` | The same `cmd` suite with the `_test-*` fixture commands compiled in. The command-contract guard walks the cobra tree, and the fixtures are only in that tree under this tag, so their classification is checked nowhere else |
| E2E | `go test -tags=e2e ./e2e/...` | Builds binary, invokes as subprocess. **No API key needed** — every case is served by an httptest stub. Runs in CI on every PR |
| Integration | `go test -count=1 -tags=integration ./integration/...` | Hits the live API. Requires `SHOVELS_API_KEY` and **fails loudly** without one |
| LLM Evals | `go test -tags=eval ./evals/... -v -timeout 60m` | Blind LLM usability tests, requires `SHOVELS_API_KEY` + `claude` CLI |

The layers answer different questions, and the distinction matters:

- **Unit and E2E verify what the CLI sends** — that the flags map to the query parameters we intend. They are deterministic, free, and run on every PR.
- **Integration verifies what the API accepts.** No stub can answer that. Both ENG-4040 and the API deploy that broke v0.8.0 slipped through a fully green unit + e2e suite, because the CLI was faithfully sending exactly what the CLI believed it should.

So an assertion belongs in `integration/` only if it depends on live API behaviour. Anything pinnable locally goes in `cmd/` or `e2e/`, which cost nothing.

### Integration (live contract suite)

```bash
SHOVELS_API_KEY=sk-... go test -count=1 -tags=integration ./integration/...
```

- Deliberately thin: a contract smoke, not a second e2e suite.
- **Cost, measured:** 9 authenticated requests (5 searches, the sentinel, contractor metrics, coverage, version) and **5 billable credits** per binary. A search costs 1 credit, a 422 costs 0, and contractor metrics, coverage, and `/meta/release` are credit-exempt. The OpenAPI canary is unauthenticated, free, and runs once in its own job rather than per binary.
- `SHOVELS_TEST_BINARY=/path/to/shovels` runs the same assertions against a downloaded release instead of a build of HEAD. That is the point: a suite that only tests HEAD stays green while the binary users are running is broken, which is exactly what happened with v0.8.0.
- Subprocesses get `CI=1` and a scratch `HOME`. `CI=1` disables the self-updater — a build of HEAD is protected by `buildVersion == "dev"`, but a released binary is not, and would otherwise replace itself mid-run.
- **`-count=1` is load-bearing.** Go caches successful test results, and neither the CLI source (built via `os/exec`) nor the live API's state is a cache input — without it a run can report an earlier success without contacting production at all.
- The unknown-tag case puts the sentinel **between two valid exclusions**. "Repeated keys return 200" proves nothing, because two valid values still return 200 when the CLI drops one. A middle sentinel means dropping in *either* direction leaves a request the API answers happily, so both first-value-wins and last-value-wins turn the test red. Exclusions specifically, because that is where a dropped value widens results.
- Runs against both HEAD and the latest stable release (`.github/workflows/integration.yml`). Not per-PR: it needs a production key in a workflow running changeable code, fork PRs get no secrets and would pass vacuously, and live-API flakiness should not block unrelated merges.

The key is an environment secret on `integration`, whose deployment branch rules limit it to `main` and `v*`. Those rules are what scope it: `workflow_dispatch` can target any ref and runs that ref's copy of the workflow, so a branch could otherwise dispatch this workflow and read the key. Naming an environment does not create them — GitHub auto-creates a referenced environment **with no protection rules**, so a repo restored from this file needs the rules configured before the secret is added.

### LLM Evals

The `evals/` directory contains LLM usability tests that verify help text is agent-friendly. Each scenario spawns a blind Claude agent with only `--help` access and a natural-language task (e.g., "Find building permits issued in Miami in 2024"). The agent must discover commands, resolve geo_ids, and produce valid results.

Run after help text changes or before releases to catch usability regressions:

```bash
SHOVELS_API_KEY=sk-... go test -tags=eval ./evals/... -v -timeout 60m
```

- Builds binary from source (tests current code, not installed version)
- 13 scenarios: permit and contractor search, geo_id resolution, metrics, contractor metrics pagination dry-run, schema and dry-run discovery, jq pipelines, zoning decisions, properties absence search
- Hard assertions: valid JSON output, correct domain (permits vs contractors), data present
- Usability rating 1-5 per scenario: advisory by default, a hard failure where `EnforceUsability` is set
- Requires a `claude` CLI with `--json-schema` support in PATH; skips gracefully if missing
- ~1.5 minutes per scenario, so ~22 minutes for a full run
- The harness pins `--model sonnet`, so a change to the `claude` CLI default moves neither the cost nor the agent's capability. Sonnet is deliberate: a stronger model can reason past help-text wording a normal agent would trip on, which is the regression these evals exist to catch.
- Model spend is ~$0.60 per scenario, so ~$8.00 for a full run. This is Anthropic API cost and is unrelated to Shovels API credits. The contractor metrics scenario is dry-run-only and adds no Shovels API credits.
- Each scenario is capped at `--max-budget-usd 3.00`. That is a runaway guard rather than a cost control — the worst observed legitimate run is ~$0.65, so a scenario approaching the cap is looping. Failed Claude JSON envelopes are logged from stdout as well as stderr so budget and structured-output failures remain diagnosable.
- The structured report must carry reproducible evidence: `final_command` is the exact complete command or jq pipeline, and `final_output` is its exact unmodified JSON stdout. Client-side JSON transformations use jq so the pipeline scenarios test the CLI's documented interoperability rather than an unrelated scripting language.
- `evals/scenarios_table_test.go` guards the scenario table and its validators with no API key, no `claude` CLI, and no network, so it still runs when `TestEval` skips

## Setup

```bash
git config core.hooksPath .githooks
```

This enables the pre-commit hook: gofmt, `go vet` including the build-tagged packages, and the unit suite. It omits the e2e suite that `.github/workflows/ci.yml` also runs, so a green hook is not a promise of green CI.

## Build & Release

```bash
# Local build
go build -o shovels .

# Run locally
./shovels --help

# Release (CI does this on tag push)
goreleaser release --clean
```

### Release Workflow

Releases are triggered by pushing a semver tag to `main`. After merging a feature PR:

```bash
git checkout main
git pull
git tag v0.X.0    # minor bump for features, patch for fixes
git push origin v0.X.0
```

The `release.yml` GitHub Action runs automatically: unit tests, then GoReleaser builds binaries and publishes the GitHub release. No manual release steps beyond the tag push.

## Geographic IDs (`--geo-id`)

The `--geo-id` flag accepts Shovels geographic identifiers. Two types keep their natural IDs:

- **Zip codes:** use the 5-digit code directly — `92024`, `90210`, `78701`
- **US states:** use the 2-letter abbreviation — `CA`, `TX`, `NY`

All other geographies have **opaque Shovels IDs** that must be resolved first using the appropriate search command:

```bash
# Resolve a city to its geo_id
shovels cities search -q "Miami" | jq '.data[0].geo_id'

# Resolve a county
shovels counties search -q "Los Angeles" | jq '.data[0].geo_id'

# Resolve a jurisdiction
shovels jurisdictions search -q "Portland" | jq '.data[0].geo_id'

# Resolve an address
shovels addresses search -q "123 Main St, Miami, FL" | jq '.data[0].geo_id'

# Then use that geo_id in a search
shovels permits search --geo-id <resolved_id> --permit-from 2024-01-01 --permit-to 2024-12-31
```

**Never fabricate geo_ids.** Formats like `CITY_LOS_ANGELES_CA` or `COUNTY_LOS_ANGELES_CA` do not exist. Always resolve through `cities search`, `counties search`, `jurisdictions search`, or `addresses search`.

## Conventions

- One cobra command file per resource in `cmd/`
- All HTTP calls go through `internal/client/` — commands never call `net/http` directly
- Flag names match API query parameter names where possible (e.g., `--permit-tags` maps to `permit_tags`)
- Use snake_case in JSON output keys (match API response format)
- Wrap API responses: `{"data": [...], "meta": {"count": N, "has_more": bool, "credits_used": N, "credits_remaining": N}}`. A capped search adds `"server_capped": N` and reports `has_more` false, since no cursor can reach past the cap
