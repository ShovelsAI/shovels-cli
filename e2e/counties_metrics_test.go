//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Happy paths ---

func TestCountiesMetricsCurrentBasic(t *testing.T) {
	handler, queries := makeMetricsHandler("/counties", 1, false, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"counties", "metrics", "current", "COUNTY_GEO_1",
		"--tag", "solar",
		"--property-type", "residential",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []struct {
		GeoID        string `json:"geo_id"`
		Tag          string `json:"tag"`
		PropertyType string `json:"property_type"`
		PermitCount  int    `json:"permit_count"`
	}
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 item, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
	hasMore := parsed.Meta["has_more"].(bool)
	if hasMore {
		t.Error("expected has_more=false")
	}

	if len(*queries) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*queries))
	}
	q := (*queries)[0]
	if q["tag"][0] != "solar" {
		t.Errorf("expected tag=solar in query, got %q", q["tag"][0])
	}
	if q["property_type"][0] != "residential" {
		t.Errorf("expected property_type=residential in query, got %q", q["property_type"][0])
	}
}

func TestCountiesMetricsMonthlyBasic(t *testing.T) {
	handler, queries := makeMetricsHandler("/counties", 3, true, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"counties", "metrics", "monthly", "COUNTY_GEO_1",
		"--tag", "solar",
		"--property-type", "residential",
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
}

// --- Error conditions ---

func TestCountiesMetricsCurrentMissingTag(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"counties", "metrics", "current", "ABC123",
		"--property-type", "residential",
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

func TestCountiesMetricsCurrentMissingPropertyType(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"counties", "metrics", "current", "ABC123",
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
	if !strings.Contains(p.Error, "--property-type") {
		t.Errorf("expected error to mention --property-type, got: %s", p.Error)
	}
}

func TestCountiesMetricsMonthlyMissingDateRange(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"counties", "metrics", "monthly", "ABC123",
		"--tag", "solar",
		"--property-type", "residential",
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

func TestCountiesMetricsCurrentRejectsDateFlags(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"counties", "metrics", "current", "ABC123",
		"--tag", "solar",
		"--property-type", "residential",
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

func TestCountiesMetricsHelpShowsSubcommands(t *testing.T) {
	result := runCLI(t, "counties", "metrics", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if !strings.Contains(result.Stdout, "current") {
		t.Error("counties metrics --help should list 'current' subcommand")
	}
	if !strings.Contains(result.Stdout, "monthly") {
		t.Error("counties metrics --help should list 'monthly' subcommand")
	}
}

func TestCountiesMetricsCurrentHelpContent(t *testing.T) {
	result := runCLI(t, "counties", "metrics", "current", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if !strings.Contains(result.Stdout, "GEO_ID") {
		t.Error("help should document GEO_ID as positional argument")
	}
	if !strings.Contains(result.Stdout, "--tag") {
		t.Error("help should list --tag flag")
	}
	if !strings.Contains(result.Stdout, "--property-type") {
		t.Error("help should list --property-type flag")
	}
	if strings.Contains(result.Stdout, "--metric-from") {
		t.Error("current --help should NOT show --metric-from")
	}
	if strings.Contains(result.Stdout, "--metric-to") {
		t.Error("current --help should NOT show --metric-to")
	}
	if !strings.Contains(result.Stdout, "counties search") {
		t.Error("help should show workflow using counties search to resolve geo_id")
	}
}

func TestCountiesMetricsMonthlyHelpContent(t *testing.T) {
	result := runCLI(t, "counties", "metrics", "monthly", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	requiredFlags := []string{"--tag", "--property-type", "--metric-from", "--metric-to"}
	for _, flag := range requiredFlags {
		if !strings.Contains(result.Stdout, flag) {
			t.Errorf("monthly --help should list %s flag", flag)
		}
	}
	if !strings.Contains(result.Stdout, "GEO_ID") {
		t.Error("help should document GEO_ID as positional argument")
	}
}
