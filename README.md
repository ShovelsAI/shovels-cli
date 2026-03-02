# Shovels CLI

[![CI](https://github.com/ShovelsAI/shovels-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/ShovelsAI/shovels-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ShovelsAI/shovels-cli)](https://github.com/ShovelsAI/shovels-cli/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/shovels-ai/shovels-cli)](https://goreportcard.com/report/github.com/shovels-ai/shovels-cli)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Agent-first CLI for the [Shovels](https://www.shovels.ai/) building permit and contractor API. A single binary that any AI agent (or human) can shell out to. JSON only. Zero interactivity.

## What is this

[Shovels](https://www.shovels.ai/) indexes U.S. building permits, contractors, and property data into a single REST API. This CLI wraps that API so you can query it from the command line.

**Designed for AI agents.** Every command prints valid JSON to stdout and structured JSON errors to stderr. No prompts, spinners, colors, or interactive elements. Help text is written for LLMs. Exit codes are meaningful and documented.

**Designed for scripts.** Pipe output to `jq`, feed it to another process, or parse it in any language. The `--limit` flag abstracts away cursor-based pagination so you never deal with page tokens.

**What you can do:**
- Search building permits by location, date range, work type, property value
- Search and filter contractors by geography, classification, metrics
- Look up addresses, contractor employees, monthly performance metrics
- Track your API credit usage

Get an API key at [shovels.ai](https://www.shovels.ai/) to get started.

## Install

```bash
curl -LsSf https://shovels.ai/install.sh | sh
```

Downloads the latest release, verifies the SHA256 checksum, installs to `~/.shovels/bin`, and adds it to your PATH. Supports macOS and Linux (amd64 / arm64).

Or download a binary directly from the [Releases](https://github.com/ShovelsAI/shovels-cli/releases/latest) page (macOS, Linux, Windows).

### Verify

```bash
shovels version
```

## Quick start

```bash
# 1. Set your API key
export SHOVELS_API_KEY=your-api-key

# 2. Search permits
shovels permits search --geo-id 90210 --permit-from 2024-01-01 --permit-to 2024-12-31

# 3. Search contractors
shovels contractors search --geo-id 90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --tags solar

# 4. Check credit usage
shovels usage
```

Or save the API key to the config file so you don't need the env var:

```bash
shovels config set api-key your-api-key
```

## Authentication

| Priority | Source | Example |
|----------|--------|---------|
| 1 | `SHOVELS_API_KEY` env var | `export SHOVELS_API_KEY=sk-abc` |
| 2 | Config file | `~/.config/shovels/config.yaml` |

## Commands

```
shovels
├── permits
│   ├── search      Search building permits by location, date, type, value
│   └── get         Retrieve one or more permits by ID
├── contractors
│   ├── search      Search contractors by location and filters
│   ├── get         Retrieve one or more contractors by ID
│   ├── permits     List permits filed by a contractor
│   ├── employees   List employees of a contractor
│   └── metrics     Monthly performance metrics for a contractor
├── addresses
│   └── search      Search addresses by street, city, state, or zip
├── usage           Show API credit usage
├── config
│   ├── set         Save a configuration value
│   └── show        Display resolved configuration (API key masked)
└── version         Print CLI version, git commit, and build date
```

### permits

Search and retrieve building permits.

```bash
# Search by location and date range
shovels permits search --geo-id 90210 --permit-from 2024-01-01 --permit-to 2024-12-31

# Filter by work type (AND logic for multiple tags)
shovels permits search --geo-id 90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --tags solar --tags roofing

# Exclude a tag (prefix with -)
shovels permits search --geo-id 90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --tags solar --tags=-roofing

# Filter by property type and minimum job value
shovels permits search --geo-id CA --permit-from 2024-01-01 --permit-to 2024-12-31 --property-type residential --min-job-value 50000

# Request total result count in meta
shovels permits search --geo-id 90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --include-count

# Retrieve permits by ID (1–50 IDs)
shovels permits get P123
shovels permits get P123 P456 P789
```

**Required flags:** `--geo-id`, `--permit-from`, `--permit-to`

**Optional flags:** `--include-count` returns `total_count` in meta (capped at 10,000)

Geographic IDs: zip codes (`90210`), states (`CA`), or Shovels IDs resolved via `shovels addresses search -q "..."`

### contractors

Search contractors and retrieve their permits, employees, and metrics.

```bash
# Search by location
shovels contractors search --geo-id 90210 --permit-from 2024-01-01 --permit-to 2024-12-31

# Filter by classification
shovels contractors search --geo-id 90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --contractor-classification general_building

# Retrieve by ID
shovels contractors get C123

# List permits filed by a contractor
shovels contractors permits ABC123 --limit 100

# List employees
shovels contractors employees ABC123

# Monthly metrics
shovels contractors metrics ABC123 \
  --metric-from 2024-01-01 --metric-to 2024-12-31 \
  --property-type residential --tag solar
```

**Search required flags:** `--geo-id`, `--permit-from`, `--permit-to`

**Search optional flags:** `--include-count` returns `total_count` in meta (capped at 10,000)

**Metrics required flags:** `--metric-from`, `--metric-to`, `--property-type`, `--tag`

### addresses

```bash
shovels addresses search --query "123 Main St"
shovels addresses search -q "San Francisco"
shovels addresses search --query "90210" --limit 10
```

**Required flags:** `--query` (or `-q`)

### usage

```bash
shovels usage
```

### config

```bash
shovels config set api-key sk-your-key
shovels config show
```

Settings are stored in `~/.config/shovels/config.yaml`.

### version

```bash
shovels version
```

## Output format

Every command writes valid JSON to **stdout**. Errors go to **stderr** as JSON. stdout is always parseable.

**Paginated:**
```json
{
  "data": [...],
  "meta": {
    "count": 25,
    "has_more": true,
    "credits_used": 1,
    "credits_remaining": 9999
  }
}
```

**Single object:**
```json
{
  "data": {...},
  "meta": {
    "credits_used": 1,
    "credits_remaining": 9999
  }
}
```

**Error (stderr):**
```json
{
  "error": "API key not configured. Set SHOVELS_API_KEY or run: shovels config set api-key YOUR_KEY",
  "code": 2,
  "error_type": "auth_error"
}
```

Possible `error_type` values: `client_error`, `validation_error`, `auth_error`, `rate_limited`, `credit_exhausted`, `server_error`, `network_error`.

## Pagination

The `--limit` flag abstracts cursor-based pagination.

- `--limit 10` — return at most 10 records
- `--limit all` — fetch all records up to `--max-records` (default 10,000)
- Hard ceiling: 100,000 records

## Global flags

| Flag | Default | Description |
|------|---------|-------------|
| `--limit` | `50` | Max records: 1–100000 or `all` |
| `--max-records` | `10000` | Cap for `--limit all` (1–100000) |
| `--base-url` | `https://api.shovels.ai/v2` | API base URL |
| `--no-retry` | `false` | Disable retry on HTTP 429 |
| `--timeout` | `30s` | Per-request timeout |

## Exit codes

| Code | Meaning | `error_type` |
|------|---------|--------------|
| 0 | Success | — |
| 1 | Client error | `client_error`, `validation_error` |
| 2 | Auth error | `auth_error` |
| 3 | Rate limited | `rate_limited` |
| 4 | Credits exhausted | `credit_exhausted` |
| 5 | Server / network error | `server_error`, `network_error` |

## API reference

This CLI wraps the [Shovels REST API v2](https://api.shovels.ai/v2/openapi.json). See the [API documentation](https://docs.shovels.ai/) for response fields, filter values, and geographic ID formats.

## License

[MIT](LICENSE)
