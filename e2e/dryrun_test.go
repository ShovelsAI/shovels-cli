//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// dryRunOutput mirrors the JSON structure of --dry-run output.
type dryRunOutput struct {
	Method string         `json:"method"`
	URL    string         `json:"url"`
	Params map[string]any `json:"params"`
}

func parseDryRun(t *testing.T, stdout string) dryRunOutput {
	t.Helper()
	var out dryRunOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("stdout is not valid dry-run JSON: %v\nstdout: %s", err, stdout)
	}
	return out
}

// =======================================================================
// Happy paths
// =======================================================================

func TestDryRunPermitsSearch(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"permits", "search",
		"--geo-id", "92024",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--tags", "solar",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)

	if out.Method != "GET" {
		t.Errorf("expected method GET, got %q", out.Method)
	}
	if !strings.HasSuffix(out.URL, "/permits/search") {
		t.Errorf("expected URL ending with /permits/search, got %q", out.URL)
	}
	if out.Params["geo_id"] != "92024" {
		t.Errorf("expected geo_id=92024, got %v", out.Params["geo_id"])
	}
	if out.Params["permit_from"] != "2024-01-01" {
		t.Errorf("expected permit_from=2024-01-01, got %v", out.Params["permit_from"])
	}
	if out.Params["permit_to"] != "2024-12-31" {
		t.Errorf("expected permit_to=2024-12-31, got %v", out.Params["permit_to"])
	}

	// permit_tags should be an array with one element.
	tags, ok := out.Params["permit_tags"].([]any)
	if !ok {
		t.Fatalf("expected permit_tags to be array, got %T: %v", out.Params["permit_tags"], out.Params["permit_tags"])
	}
	if len(tags) != 1 || tags[0] != "solar" {
		t.Errorf("expected permit_tags=[solar], got %v", tags)
	}

	// size should be 50 (integer).
	size, ok := out.Params["size"].(float64)
	if !ok {
		t.Fatalf("expected size to be number, got %T: %v", out.Params["size"], out.Params["size"])
	}
	if int(size) != 50 {
		t.Errorf("expected size=50, got %v", size)
	}
}

func TestDryRunMetricsCurrentPathInterpolation(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"cities", "metrics", "current", "ABC123",
		"--tag", "solar",
		"--property-type", "residential",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)

	if !strings.HasSuffix(out.URL, "/cities/ABC123/metrics/current") {
		t.Errorf("expected URL with interpolated path, got %q", out.URL)
	}
	if out.Params["tag"] != "solar" {
		t.Errorf("expected tag=solar, got %v", out.Params["tag"])
	}
	if out.Params["property_type"] != "residential" {
		t.Errorf("expected property_type=residential, got %v", out.Params["property_type"])
	}
}

func TestDryRunZeroAPICalls(t *testing.T) {
	var apiCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "92024",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if got := apiCalls.Load(); got != 0 {
		t.Errorf("expected 0 API calls with --dry-run, got %d", got)
	}
}

// =======================================================================
// Edge cases
// =======================================================================

func TestDryRunBaseURLOverride(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", "https://custom.api.example/v3",
		"permits", "search",
		"--geo-id", "92024",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	if !strings.HasPrefix(out.URL, "https://custom.api.example/v3") {
		t.Errorf("expected URL with custom base, got %q", out.URL)
	}
}

func TestDryRunLimitAllShowsSize100(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--limit", "all",
		"permits", "search",
		"--geo-id", "92024",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	size, ok := out.Params["size"].(float64)
	if !ok {
		t.Fatalf("expected size to be number, got %T: %v", out.Params["size"], out.Params["size"])
	}
	if int(size) != 100 {
		t.Errorf("expected size=100 for --limit all, got %v", size)
	}
}

func TestDryRunVersionNoEffect(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env, "version", "--dry-run")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Version command should produce its normal JSON output, not dry-run output.
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if _, ok := envelope.Data["version"]; !ok {
		t.Error("expected version field in data, not dry-run output")
	}
}

func TestDryRunConfigNoEffect(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env, "config", "show", "--dry-run")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Config show should produce its normal JSON output.
	var envelope struct {
		Data struct {
			BaseURL string `json:"base_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if envelope.Data.BaseURL == "" {
		t.Error("expected base_url in config show output, not dry-run output")
	}
}

// =======================================================================
// Error conditions
// =======================================================================

func TestDryRunMissingRequiredFlags(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"permits", "search",
		"--dry-run",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "--geo-id") {
		t.Errorf("expected error to mention --geo-id, got: %s", p.Error)
	}
}

func TestDryRunInvalidDateFormat(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"permits", "search",
		"--geo-id", "92024",
		"--permit-from", "01/01/2024",
		"--permit-to", "2024-12-31",
		"--dry-run",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "invalid date format") {
		t.Errorf("expected error about invalid date format, got: %s", p.Error)
	}
}

func TestDryRunInvalidTimeoutPaginated(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"permits", "search",
		"--geo-id", "92024",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--dry-run",
		"--timeout", "banana",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "invalid timeout") {
		t.Errorf("expected error about invalid timeout, got: %s", p.Error)
	}
}

func TestDryRunInvalidTimeoutNonPaginated(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"usage",
		"--dry-run",
		"--timeout", "notaduration",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "invalid timeout") {
		t.Errorf("expected error about invalid timeout, got: %s", p.Error)
	}
}

// =======================================================================
// Boundary conditions
// =======================================================================

func TestDryRunNoAuthRequired(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env,
		"permits", "search",
		"--geo-id", "92024",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 without auth, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Should produce valid dry-run output even without an API key.
	out := parseDryRun(t, result.Stdout)
	if out.Method != "GET" {
		t.Errorf("expected method GET, got %q", out.Method)
	}
}

func TestDryRunNoAuthHeaders(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"permits", "search",
		"--geo-id", "92024",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// The output must not contain any auth-related data.
	if strings.Contains(result.Stdout, "sk-test") {
		t.Error("dry-run output must not contain the API key")
	}
	if strings.Contains(result.Stdout, "X-API-Key") {
		t.Error("dry-run output must not contain auth header names")
	}
	if strings.Contains(result.Stdout, "Authorization") {
		t.Error("dry-run output must not contain Authorization header")
	}
}

// =======================================================================
// Additional coverage: various command types
// =======================================================================

func TestDryRunContractorsGet(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "get", "C123", "C456",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	if !strings.HasSuffix(out.URL, "/contractors") {
		t.Errorf("expected URL ending with /contractors, got %q", out.URL)
	}

	// IDs should be in the params as a multi-value array.
	ids, ok := out.Params["id"].([]any)
	if !ok {
		t.Fatalf("expected id to be array, got %T: %v", out.Params["id"], out.Params["id"])
	}
	if len(ids) != 2 || ids[0] != "C123" || ids[1] != "C456" {
		t.Errorf("expected id=[C123 C456], got %v", ids)
	}
}

func TestDryRunContractorsMetrics(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "metrics", "C123",
		"--metric-from", "2024-01-01",
		"--metric-to", "2024-12-31",
		"--property-type", "residential",
		"--tag", "solar",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	if !strings.HasSuffix(out.URL, "/contractors/C123/metrics") {
		t.Errorf("expected URL with interpolated ID, got %q", out.URL)
	}
	if out.Params["tag"] != "solar" {
		t.Errorf("expected tag=solar, got %v", out.Params["tag"])
	}
}

func TestDryRunContractorsPermits(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "permits", "C123",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	if !strings.HasSuffix(out.URL, "/contractors/C123/permits") {
		t.Errorf("expected URL with interpolated ID, got %q", out.URL)
	}
}

func TestDryRunContractorsEmployees(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "employees", "C123",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	if !strings.HasSuffix(out.URL, "/contractors/C123/employees") {
		t.Errorf("expected URL with interpolated ID, got %q", out.URL)
	}
}

func TestDryRunTagsList(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"tags", "list",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	if !strings.HasSuffix(out.URL, "/list/tags") {
		t.Errorf("expected URL ending with /list/tags, got %q", out.URL)
	}
}

func TestDryRunUsage(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"usage",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	if !strings.HasSuffix(out.URL, "/usage") {
		t.Errorf("expected URL ending with /usage, got %q", out.URL)
	}
}

func TestDryRunAddressesResidents(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"addresses", "residents", "ADDR123",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	if !strings.HasSuffix(out.URL, "/addresses/ADDR123/residents") {
		t.Errorf("expected URL with interpolated ID, got %q", out.URL)
	}
}

func TestDryRunCitiesSearch(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"cities", "search", "-q", "Miami",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	if !strings.HasSuffix(out.URL, "/cities/search") {
		t.Errorf("expected URL ending with /cities/search, got %q", out.URL)
	}
	if out.Params["q"] != "Miami" {
		t.Errorf("expected q=Miami, got %v", out.Params["q"])
	}
}

func TestDryRunMetricsMonthly(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"counties", "metrics", "monthly", "GEO1",
		"--tag", "solar",
		"--property-type", "residential",
		"--metric-from", "2024-01-01",
		"--metric-to", "2024-12-31",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	if !strings.HasSuffix(out.URL, "/counties/GEO1/metrics/monthly") {
		t.Errorf("expected URL with interpolated path, got %q", out.URL)
	}
	if out.Params["metric_from"] != "2024-01-01" {
		t.Errorf("expected metric_from=2024-01-01, got %v", out.Params["metric_from"])
	}
}

func TestDryRunSmallLimitShowsCorrectSize(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--limit", "10",
		"permits", "search",
		"--geo-id", "92024",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	size, ok := out.Params["size"].(float64)
	if !ok {
		t.Fatalf("expected size to be number, got %T: %v", out.Params["size"], out.Params["size"])
	}
	if int(size) != 10 {
		t.Errorf("expected size=10 for --limit 10, got %v", size)
	}
}

func TestDryRunLargeLimitCapsAtPageMax(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--limit", "200",
		"permits", "search",
		"--geo-id", "92024",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	size, ok := out.Params["size"].(float64)
	if !ok {
		t.Fatalf("expected size to be number, got %T: %v", out.Params["size"], out.Params["size"])
	}
	if int(size) != 100 {
		t.Errorf("expected size=100 for --limit 200, got %v", size)
	}
}
