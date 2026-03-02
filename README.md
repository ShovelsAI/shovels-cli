# shovels

Agent-first CLI for the [Shovels](https://www.shovels.ai/) building permit and contractor API. A single binary that any AI agent (or human) can use via shell commands. JSON output, predictable error codes, zero interactivity.

## Install

### Homebrew (macOS/Linux)

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

Download the archive for your platform from [GitHub Releases](https://github.com/shovels-ai/shovels-cli/releases/latest), extract it, and place the `shovels` binary on your PATH.

```bash
# Example: macOS arm64
curl -sL https://github.com/shovels-ai/shovels-cli/releases/latest/download/shovels_0.1.0_darwin_arm64.tar.gz | tar xz
sudo mv shovels /usr/local/bin/
```

### Verify

```bash
shovels version
```

## Quick start

### 1. Set your API key

```bash
shovels config set api-key YOUR_API_KEY
```

Or export it as an environment variable:

```bash
export SHOVELS_API_KEY=YOUR_API_KEY
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
| 1 | `--api-key` flag | `shovels permits search --api-key sk-abc ...` |
| 2 | `SHOVELS_API_KEY` env var | `export SHOVELS_API_KEY=sk-abc` |
| 3 | Config file | `~/.config/shovels/config.yaml` |

## Output format

Every command writes valid JSON to stdout. Errors go to stderr as JSON. Pipe to `jq`, parse programmatically, or feed to another AI agent.

Paginated responses:

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

Single-object responses:

```json
{
  "data": {...},
  "meta": {
    "credits_used": 1,
    "credits_remaining": 9999
  }
}
```

## Commands

### permits

Search and retrieve building permits.

```bash
# Search permits by location, date range, and tags
shovels permits search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --tags solar

# Multiple tags (AND logic)
shovels permits search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --tags solar --tags roofing

# Exclude a tag (prefix with -)
shovels permits search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --tags solar --tags=-roofing

# Filter by property type and minimum value
shovels permits search --geo-id STATE_CA --permit-from 2024-01-01 --permit-to 2024-12-31 --property-type residential --min-job-value 50000

# Retrieve permits by ID
shovels permits get P123
shovels permits get P123 P456 P789
```

**Search required flags:** `--geo-id`, `--permit-from`, `--permit-to`

Geographic ID formats: `ZIP_90210`, `CITY_LOS_ANGELES_CA`, `COUNTY_LOS_ANGELES_CA`, `STATE_CA`

### contractors

Search contractors and retrieve their permits, employees, and metrics.

```bash
# Search contractors
shovels contractors search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31

# Filter by classification
shovels contractors search --geo-id ZIP_90210 --permit-from 2024-01-01 --permit-to 2024-12-31 --contractor-classification general_building

# Retrieve contractors by ID
shovels contractors get C123
shovels contractors get C123 C456

# List permits filed by a contractor
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
shovels config set base-url https://staging.shovels.ai/v2

# Show resolved config (API key masked)
shovels config show
```

### version

Print CLI version, git commit, and build date.

```bash
shovels version
```

## Global flags

| Flag | Default | Description |
|------|---------|-------------|
| `--api-key` | | API key (overrides env var and config file) |
| `--limit` | `50` | Maximum records: integer 1-100000 or `all` |
| `--max-records` | `10000` | Upper bound when `--limit=all`, range 1-100000 |
| `--base-url` | `https://api.shovels.ai/v2` | API base URL |
| `--no-retry` | `false` | Disable automatic retry on rate-limit (HTTP 429) |
| `--timeout` | `30s` | Per-request timeout (Go duration: `10s`, `1m`, `2m30s`) |

## Pagination

The `--limit` flag abstracts cursor-based pagination. The CLI handles page mechanics internally.

- `--limit 10` returns at most 10 records
- `--limit all` fetches all records up to the `--max-records` cap (default 10,000)
- The hard ceiling is 100,000 records regardless of `--max-records`

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Client error (invalid flags, validation failure) |
| 2 | Authentication error (missing or invalid API key) |
| 3 | Rate limit exceeded (HTTP 429) |
| 4 | Credits exhausted |
| 5 | Transient server or network error |

## License

[MIT](LICENSE)
