# Shovels CLI

[![CI](https://github.com/ShovelsAI/shovels-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/ShovelsAI/shovels-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/ShovelsAI/shovels-cli)](https://github.com/ShovelsAI/shovels-cli/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/shovels-ai/shovels-cli)](https://goreportcard.com/report/github.com/shovels-ai/shovels-cli)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Agent-first CLI for the [Shovels](https://www.shovels.ai/) building permit and contractor API. A single binary that any AI agent (or human) can shell out to. JSON only. Zero interactivity.

## What is this

[Shovels](https://www.shovels.ai/) indexes U.S. building permits, contractors, and property data into a single REST API. This CLI wraps that API so you can query it from the command line.

**Designed for AI agents.** Every command prints valid JSON to stdout and structured JSON errors to stderr. There are no prompts, spinners, colors, or interactive elements. Help text is written for LLMs — specific, example-rich, no jargon. Exit codes are meaningful and documented.

**Designed for scripts.** Pipe output to `jq`, feed it to another process, or parse it in any language. The `--limit` flag abstracts away cursor-based pagination so you never deal with page tokens.

**What you can do:**
- Search building permits by location, date range, work type, property value
- Search and filter contractors by geography, classification, metrics
- Look up addresses, contractor employees, monthly performance metrics
- Track your API credit usage

Get an API key at [shovels.ai](https://www.shovels.ai/) to get started.

## Install

### Homebrew (macOS / Linux)

```bash
brew install shovels-ai/tap/shovels
```

### go install

```bash
go install github.com/shovels-ai/shovels-cli@latest
```

The binary installs as `shovels-cli`. Rename it if desired:

```bash
mv $(go env GOPATH)/bin/shovels-cli $(go env GOPATH)/bin/shovels
```

### Download binary

Prebuilt binaries for macOS, Linux, and Windows are available on the [Releases](https://github.com/shovels-ai/shovels-cli/releases/latest) page.

| Platform | Archive |
|----------|---------|
| macOS (Apple Silicon) | `shovels_{version}_darwin_arm64.tar.gz` |
| macOS (Intel) | `shovels_{version}_darwin_amd64.tar.gz` |
| Linux (x86_64) | `shovels_{version}_linux_amd64.tar.gz` |
| Linux (ARM64) | `shovels_{version}_linux_arm64.tar.gz` |
| Windows (x86_64) | `shovels_{version}_windows_amd64.zip` |

```bash
# Example: macOS Apple Silicon, replace VERSION with the latest release tag
curl -sL https://github.com/shovels-ai/shovels-cli/releases/download/VERSION/shovels_VERSION_darwin_arm64.tar.gz | tar xz
sudo mv shovels /usr/local/bin/
```

### Verify

```bash
shovels version
```

## Quick start

### 1. Set your API key

```bash
export SHOVELS_API_KEY=your-api-key
```

Or save it to the config file:

```bash
shovels config set api-key your-api-key
```

### 2. Search permits

```bash
shovels permits search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31
```

### 3. Search contractors

```bash
shovels contractors search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --tags solar
```

### 4. Check credit usage

```bash
shovels usage
```

## Authentication

The CLI resolves the API key in this order:

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
# Search permits by location and date range
shovels permits search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31

# Filter by work type (AND logic for multiple tags)
shovels permits search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --tags solar --tags roofing

# Exclude a tag (prefix with -)
shovels permits search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --tags solar --tags=-roofing

# Filter by property type and minimum job value
shovels permits search --geo-id STATE_CA --permit-from 2024-01-01 --permit-to 2024-12-31 --property-type residential --min-job-value 50000

# Request total result count in meta
shovels permits search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --include-count

# Retrieve permits by ID (1-50 IDs)
shovels permits get P123
shovels permits get P123 P456 P789
```

**Search required flags:** `--geo-id`, `--permit-from`, `--permit-to`

**Search optional flags:** `--include-count` requests total result count (capped at 10,000), returned as `total_count` in meta

Geographic ID formats: `ZIP_90210`, `CITY_LOS_ANGELES_CA`, `COUNTY_LOS_ANGELES_CA`, `STATE_CA`

### contractors

Search contractors and retrieve their permits, employees, and metrics.

```bash
# Search contractors by location
shovels contractors search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31

# Filter by classification
shovels contractors search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --contractor-classification general_building

# Retrieve contractors by ID
shovels contractors get C123
shovels contractors get C123 C456

# List permits filed by a contractor (paginated)
shovels contractors permits ABC123
shovels contractors permits ABC123 --limit 100

# List employees of a contractor
shovels contractors employees ABC123

# Get monthly metrics for a contractor
shovels contractors metrics ABC123 \
  --metric-from 2024-01-01 --metric-to 2024-12-31 \
  --property-type residential --tag solar
```

**Search required flags:** `--geo-id`, `--permit-from`, `--permit-to`

**Search optional flags:** `--include-count` requests total result count (capped at 10,000), returned as `total_count` in meta

**Metrics required flags:** `--metric-from`, `--metric-to`, `--property-type`, `--tag`

### addresses

Search addresses by street, city, state, or zip code.

```bash
shovels addresses search --query "123 Main St"
shovels addresses search -q "San Francisco"
shovels addresses search --query "90210" --limit 10
```

**Required flags:** `--query` (or `-q`)

### usage

Check API credit consumption.

```bash
shovels usage
```

### config

Manage persistent CLI settings stored in `~/.config/shovels/config.yaml`.

```bash
# Save API key
shovels config set api-key sk-your-key

# Override base URL
shovels config set base-url https://api.example.com/v2

# Show resolved config (API key masked)
shovels config show
```

### version

Print CLI version, git commit, and build date.

```bash
shovels version
```

## Output format

Every command writes valid JSON to **stdout**. Errors go to **stderr** as JSON. This means stdout is always parseable — pipe it to `jq`, feed it to another process, or hand it to an AI agent.

### Paginated responses

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

### Single-object responses

```json
{
  "data": {...},
  "meta": {
    "credits_used": 1,
    "credits_remaining": 9999
  }
}
```

### Errors (written to stderr)

```json
{
  "error": "API key not configured. Set SHOVELS_API_KEY or run: shovels config set api-key YOUR_KEY",
  "code": 2,
  "error_type": "auth_error"
}
```

The `error_type` field is machine-readable. Possible values: `client_error`, `validation_error`, `auth_error`, `rate_limited`, `credit_exhausted`, `server_error`, `network_error`.

## Pagination

The `--limit` flag abstracts cursor-based pagination. The CLI handles page mechanics internally.

- `--limit 10` — return at most 10 records
- `--limit all` — fetch all records up to the `--max-records` cap (default 10,000)
- Hard ceiling: 100,000 records regardless of `--max-records`

## Global flags

| Flag | Default | Description |
|------|---------|-------------|
| `--limit` | `50` | Maximum records to return: integer 1–100000 or `all` |
| `--max-records` | `10000` | Upper bound when `--limit all` (range 1–100000) |
| `--base-url` | `https://api.shovels.ai/v2` | API base URL |
| `--no-retry` | `false` | Disable automatic retry on rate-limit (HTTP 429) |
| `--timeout` | `30s` | Per-request timeout (Go duration: `10s`, `1m`, `2m30s`) |

## Exit codes

| Code | Meaning | `error_type` |
|------|---------|--------------|
| 0 | Success | — |
| 1 | Client error (invalid flags, validation failure) | `client_error`, `validation_error` |
| 2 | Authentication error (missing or invalid API key) | `auth_error` |
| 3 | Rate limit exceeded (HTTP 429) | `rate_limited` |
| 4 | Credits exhausted | `credit_exhausted` |
| 5 | Transient server or network error | `server_error`, `network_error` |

## API reference

This CLI wraps the [Shovels REST API v2](https://api.shovels.ai/v2/openapi.json). See the [API documentation](https://docs.shovels.ai/) for details on response fields, filter values, and geographic ID formats.

## Contributing

```bash
# Build
go build -o shovels .

# Run tests
go test ./...

# Check formatting
gofmt -l .
go vet ./...
```

See [CLAUDE.md](CLAUDE.md) for architecture details and conventions.

## License

[MIT](LICENSE)
