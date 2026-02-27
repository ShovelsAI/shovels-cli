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
- **Meaningful exit codes:** 0=success, 1=client error, 2=auth error, 3=rate-limit, 4=credits-exhausted.
- **`--help` text is for LLMs.** Write descriptions as if an AI agent is reading them — specific, example-rich, no jargon.

## Auth Precedence

`--api-key` flag > `SHOVELS_API_KEY` env var > `~/.config/shovels/config.yaml`

## Testing

| Layer | Command | Notes |
|-------|---------|-------|
| Unit | `go test ./...` | No network calls, mock HTTP client |
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

## Conventions

- One cobra command file per resource in `cmd/`
- All HTTP calls go through `internal/client/` — commands never call `net/http` directly
- Flag names match API query parameter names where possible (e.g., `--permit-tags` maps to `permit_tags`)
- Use snake_case in JSON output keys (match API response format)
- Wrap API responses: `{"items": [...], "count": N, "has_more": bool, "credits_used": N, "credits_remaining": N}`

## Linear

- Tracking issue: [ENG-1889](https://linear.app/shovels/issue/ENG-1889)
