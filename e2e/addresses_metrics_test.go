//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Happy paths ---

func TestAddressesMetricsCurrentBasic(t *testing.T) {
	handler, queries := makeMetricsHandler("/addresses", 1, false, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"addresses", "metrics", "current", "ADDR_GEO_1",
		"--tag", "solar",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// Unmarshal into typed struct to verify expected fields.
	var data []struct {
		GeoID       string `json:"geo_id"`
		Tag         string `json:"tag"`
		PermitCount int    `json:"permit_count"`
	}
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 item, got %d", len(data))
	}

	// Verify property_type is absent from each response item.
	var rawItems []map[string]any
	if err := json.Unmarshal(parsed.Data, &rawItems); err != nil {
		t.Fatalf("expected data array of objects: %v", err)
	}
	for i, item := range rawItems {
		if _, hasPT := item["property_type"]; hasPT {
			t.Errorf("data[%d]: addresses metrics should NOT contain property_type key", i)
		}
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
	hasMore := parsed.Meta["has_more"].(bool)
	if hasMore {
		t.Error("expected has_more=false")
	}

	// Verify query params: tag present, NO property_type.
	if len(*queries) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*queries))
	}
	q := (*queries)[0]
	if q["tag"][0] != "solar" {
		t.Errorf("expected tag=solar in query, got %q", q["tag"][0])
	}
	if _, hasPT := q["property_type"]; hasPT {
		t.Error("addresses metrics should NOT send property_type query param")
	}
}

func TestAddressesMetricsMonthlyBasic(t *testing.T) {
	handler, queries := makeMetricsHandler("/addresses", 3, true, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"addresses", "metrics", "monthly", "ADDR_GEO_1",
		"--tag", "solar",
		"--metric-from", "2024-01-01",
		"--metric-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []struct {
		Date        string `json:"date"`
		PermitCount int    `json:"permit_count"`
	}
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 3 {
		t.Fatalf("expected 3 items, got %d", len(data))
	}
	for i, item := range data {
		if item.Date == "" {
			t.Errorf("data[%d]: expected non-empty date field", i)
		}
	}

	// Verify property_type is absent from each monthly response item.
	var rawItems []map[string]any
	if err := json.Unmarshal(parsed.Data, &rawItems); err != nil {
		t.Fatalf("expected data array of objects: %v", err)
	}
	for i, item := range rawItems {
		if _, hasPT := item["property_type"]; hasPT {
			t.Errorf("data[%d]: addresses monthly metrics should NOT contain property_type key", i)
		}
	}

	// Verify date range and no property_type.
	if len(*queries) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*queries))
	}
	q := (*queries)[0]
	if q["metric_from"][0] != "2024-01-01" {
		t.Errorf("expected metric_from=2024-01-01, got %q", q["metric_from"][0])
	}
	if q["metric_to"][0] != "2024-12-31" {
		t.Errorf("expected metric_to=2024-12-31, got %q", q["metric_to"][0])
	}
	if _, hasPT := q["property_type"]; hasPT {
		t.Error("addresses monthly metrics should NOT send property_type query param")
	}
}

// --- Edge cases ---

func TestAddressesMetricsCurrentRejectsPropertyType(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"addresses", "metrics", "current", "ADDR_GEO_1",
		"--tag", "solar",
		"--property-type", "residential",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Cobra rejects unknown flags with a message containing the flag name.
	if !strings.Contains(result.Stderr, "property-type") {
		t.Errorf("expected stderr to mention --property-type as unknown flag, got: %s", result.Stderr)
	}
}

func TestAddressesMetricsMonthlyRejectsPropertyType(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"addresses", "metrics", "monthly", "ADDR_GEO_1",
		"--tag", "solar",
		"--metric-from", "2024-01-01",
		"--metric-to", "2024-12-31",
		"--property-type", "residential",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if !strings.Contains(result.Stderr, "property-type") {
		t.Errorf("expected stderr to mention --property-type as unknown flag, got: %s", result.Stderr)
	}
}

// --- Error conditions ---

func TestAddressesMetricsCurrentMissingTag(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"addresses", "metrics", "current", "ABC123",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "--tag") {
		t.Errorf("expected error to mention --tag, got: %s", p.Error)
	}
}

func TestAddressesMetricsMonthlyMissingTag(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"addresses", "metrics", "monthly", "ABC123",
		"--metric-from", "2024-01-01",
		"--metric-to", "2024-12-31",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "--tag") {
		t.Errorf("expected error to mention --tag, got: %s", p.Error)
	}
}

func TestAddressesMetricsMonthlyMissingDateRange(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"addresses", "metrics", "monthly", "ABC123",
		"--tag", "solar",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "--metric-from") {
		t.Errorf("expected error to mention --metric-from, got: %s", p.Error)
	}
	if !strings.Contains(p.Error, "--metric-to") {
		t.Errorf("expected error to mention --metric-to, got: %s", p.Error)
	}
}

func TestAddressesMetricsCurrentRejectsDateFlags(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"addresses", "metrics", "current", "ABC123",
		"--tag", "solar",
		"--metric-from", "2024-01-01",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "flags --metric-from/--metric-to are only valid on the monthly variant") {
		t.Errorf("expected date flag rejection message, got: %s", p.Error)
	}
}

// --- Boundary conditions ---

func TestAddressesMetricsHelpShowsSubcommands(t *testing.T) {
	result := runCLI(t, "addresses", "metrics", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if !strings.Contains(result.Stdout, "current") {
		t.Error("addresses metrics --help should list 'current' subcommand")
	}
	if !strings.Contains(result.Stdout, "monthly") {
		t.Error("addresses metrics --help should list 'monthly' subcommand")
	}
}

func TestAddressesMetricsCurrentHelpContent(t *testing.T) {
	result := runCLI(t, "addresses", "metrics", "current", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if !strings.Contains(result.Stdout, "GEO_ID") {
		t.Error("help should document GEO_ID as positional argument")
	}
	if !strings.Contains(result.Stdout, "--tag") {
		t.Error("help should list --tag flag")
	}
	// Addresses metrics should NOT show --property-type.
	if strings.Contains(result.Stdout, "--property-type") {
		t.Error("addresses current --help should NOT show --property-type")
	}
	if strings.Contains(result.Stdout, "--metric-from") {
		t.Error("current --help should NOT show --metric-from")
	}
	if strings.Contains(result.Stdout, "--metric-to") {
		t.Error("current --help should NOT show --metric-to")
	}
	if !strings.Contains(result.Stdout, "addresses search") {
		t.Error("help should show workflow using addresses search to resolve geo_id")
	}
}

func TestAddressesMetricsMonthlyHelpContent(t *testing.T) {
	result := runCLI(t, "addresses", "metrics", "monthly", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	requiredFlags := []string{"--tag", "--metric-from", "--metric-to"}
	for _, flag := range requiredFlags {
		if !strings.Contains(result.Stdout, flag) {
			t.Errorf("monthly --help should list %s flag", flag)
		}
	}
	// Addresses monthly should NOT show --property-type.
	if strings.Contains(result.Stdout, "--property-type") {
		t.Error("addresses monthly --help should NOT show --property-type")
	}
	if !strings.Contains(result.Stdout, "GEO_ID") {
		t.Error("help should document GEO_ID as positional argument")
	}
}

func TestAddressesMetricsCurrentAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"detail":"Invalid API key"}`))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	env := []string{
		"XDG_CONFIG_HOME=" + tmpDir,
		"SHOVELS_API_KEY=sk-bad",
	}
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"addresses", "metrics", "current", "ABC123",
		"--tag", "solar",
	)

	if result.ExitCode != 2 {
		t.Fatalf("expected exit 2, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 2 {
		t.Errorf("expected error code 2, got %d", p.Code)
	}
	if p.ErrorType != "auth_error" {
		t.Errorf("expected error_type %q, got %q", "auth_error", p.ErrorType)
	}
}
