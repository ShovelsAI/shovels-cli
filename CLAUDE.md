# Shovels CLI — Engineering Standards

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
internal/
  client/       HTTP client (generated types, hand-crafted calls)
  config/       config file + env var resolution
  output/       JSON output formatting, error rendering
```

## Design Principles

- **JSON-only output.** No tables, no colors. Every response is valid JSON to stdout.
- **Errors to stderr.** Structured JSON errors go to stderr so stdout is always parseable.
- **`--limit N` abstracts pagination.** The CLI handles cursor mechanics internally. Default: 50. `--limit all` fetches everything.
- **Never interactive.** No prompts, no spinners, no progress bars. Fail loudly with clear messages.
- **Meaningful exit codes:** 0=success, 1=client error, 2=auth error, 3=rate-limit, 4=credits-exhausted, 5=transient/server error.
- **`--help` text is for LLMs.** Write descriptions as if an AI agent is reading them — specific, example-rich, no jargon.

## Auth Precedence

`SHOVELS_API_KEY` env var > `~/.config/shovels/config.yaml`

## Testing

| Layer | Command | Notes |
|-------|---------|-------|
| Unit | `go test ./...` | No network calls, mock HTTP client |
| E2E | `go test -tags=e2e ./e2e/...` | Builds binary, invokes as subprocess, requires `SHOVELS_API_KEY` |
| Integration | `go test -tags=integration ./...` | Hits live API, requires `SHOVELS_API_KEY` |

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
- Wrap API responses: `{"data": [...], "meta": {"count": N, "has_more": bool, "credits_used": N, "credits_remaining": N}}`