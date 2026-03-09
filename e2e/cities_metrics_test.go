//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

// makeMetricsHandler returns an HTTP handler that serves paginated geo metrics
// responses. The pathPrefix (e.g., "/cities") determines the resource type and
// controls whether property_type is included in response items (excluded for
// "/addresses"). The monthly flag controls whether each item includes a "date"
// field.
func makeMetricsHandler(pathPrefix string, totalItems int, monthly bool, creditsUsed, creditsRemaining int) (http.Handler, *[]map[string][]string) {
	var served atomic.Int32
	capturedQueries := &[]map[string][]string{}

	includePropertyType := pathPrefix != "/addresses"

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := map[string][]string{}
		for k, v := range r.URL.Query() {
			params[k] = v
		}
		*capturedQueries = append(*capturedQueries, params)

		// Validate required query params.
		if r.URL.Query().Get("tag") == "" {
			w.WriteHeader(422)
			w.Write([]byte(`{"detail":"tag is required"}`))
			return
		}

		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		if size == 0 {
			size = 50
		}

		start := int(served.Load())
		remaining := totalItems - start
		count := min(size, remaining)
		if count < 0 {
			count = 0
		}
		served.Add(int32(count))

		ptField := ""
		if includePropertyType {
			ptField = `"property_type":"residential",`
		}

		items := make([]json.RawMessage, count)
		for i := range count {
			idx := start + i
			if monthly {
				items[i] = json.RawMessage(fmt.Sprintf(
					`{"geo_id":"geo_%05d","tag":"solar",%s"date":"2024-%02d-01","permit_count":%d,"contractor_count":%d,"avg_construction_duration":45,"avg_approval_duration":12,"total_job_value":850000000,"avg_inspection_pass_rate":87,"permit_active_count":23,"permit_in_review_count":5}`,
					idx, ptField, (idx%12)+1, 100+idx, 20+idx,
				))
			} else {
				items[i] = json.RawMessage(fmt.Sprintf(
					`{"geo_id":"geo_%05d","tag":"solar",%s"permit_count":%d,"contractor_count":%d,"avg_construction_duration":45,"avg_approval_duration":12,"total_job_value":850000000,"avg_inspection_pass_rate":87,"permit_active_count":23,"permit_in_review_count":5}`,
					idx, ptField, 100+idx, 20+idx,
				))
			}
		}

		w.Header().Set("X-Credits-Request", strconv.Itoa(creditsUsed))
		w.Header().Set("X-Credits-Remaining", strconv.Itoa(creditsRemaining))

		end := start + count
		var nextCursor *string
		if end < totalItems {
			cursor := fmt.Sprintf("cursor_%d", end)
			nextCursor = &cursor
		}

		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
			TotalCount *struct {
				Value    int    `json:"value"`
				Relation string `json:"relation"`
			} `json:"total_count,omitempty"`
		}{Items: items, NextCursor: nextCursor}

		// Include total_count on first page when include_count is requested.
		if r.URL.Query().Get("include_count") == "true" && start == 0 {
			resp.TotalCount = &struct {
				Value    int    `json:"value"`
				Relation string `json:"relation"`
			}{Value: totalItems, Relation: "eq"}
		}

		json.NewEncoder(w).Encode(resp)
	})

	return handler, capturedQueries
}

// --- Happy paths ---

func TestCitiesMetricsCurrentBasic(t *testing.T) {
	handler, queries := makeMetricsHandler("/cities", 1, false, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "metrics", "current", "SfAy51LPDMc",
		"--tag", "solar",
		"--property-type", "residential",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []struct {
		GeoID                   string `json:"geo_id"`
		Tag                     string `json:"tag"`
		PropertyType            string `json:"property_type"`
		PermitCount             int    `json:"permit_count"`
		ContractorCount         int    `json:"contractor_count"`
		AvgConstructionDuration int    `json:"avg_construction_duration"`
		TotalJobValue           int    `json:"total_job_value"`
		AvgInspectionPassRate   int    `json:"avg_inspection_pass_rate"`
		PermitActiveCount       int    `json:"permit_active_count"`
		PermitInReviewCount     int    `json:"permit_in_review_count"`
	}
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array of metrics objects: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 item, got %d", len(data))
	}

	item := data[0]
	if item.Tag != "solar" {
		t.Errorf("expected tag=solar, got %q", item.Tag)
	}
	if item.PropertyType != "residential" {
		t.Errorf("expected property_type=residential, got %q", item.PropertyType)
	}
	if item.PermitCount == 0 {
		t.Error("expected non-zero permit_count")
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
	hasMore := parsed.Meta["has_more"].(bool)
	if hasMore {
		t.Error("expected has_more=false")
	}

	// Verify query params sent to API.
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

func TestCitiesMetricsMonthlyBasic(t *testing.T) {
	handler, queries := makeMetricsHandler("/cities", 3, true, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "metrics", "monthly", "SfAy51LPDMc",
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
		GeoID        string `json:"geo_id"`
		Tag          string `json:"tag"`
		PropertyType string `json:"property_type"`
		Date         string `json:"date"`
		PermitCount  int    `json:"permit_count"`
	}
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 3 {
		t.Fatalf("expected 3 items, got %d", len(data))
	}

	// Verify monthly items have date field.
	for i, item := range data {
		if item.Date == "" {
			t.Errorf("data[%d]: expected non-empty date field", i)
		}
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 3 {
		t.Errorf("expected count=3, got %d", count)
	}

	// Verify date range params sent to API.
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

func TestCitiesMetricsCurrentWithLimit(t *testing.T) {
	handler, _ := makeMetricsHandler("/cities", 20, false, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "metrics", "current", "ABC123",
		"--tag", "solar",
		"--property-type", "residential",
		"--limit", "10",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 10 {
		t.Errorf("expected 10 items with --limit 10, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 10 {
		t.Errorf("expected count=10, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if !hasMore {
		t.Error("expected has_more=true when more items exist beyond limit")
	}
}

func TestCitiesMetricsCurrentIncludeCount(t *testing.T) {
	handler, queries := makeMetricsHandler("/cities", 5, false, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "metrics", "current", "ABC123",
		"--tag", "solar",
		"--property-type", "residential",
		"--include-count",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// Verify include_count was sent to API.
	if len(*queries) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*queries))
	}
	q := (*queries)[0]
	if q["include_count"][0] != "true" {
		t.Errorf("expected include_count=true in query, got %q", q["include_count"][0])
	}

	// Verify total_count in meta.
	tc, ok := parsed.Meta["total_count"]
	if !ok {
		t.Fatal("expected total_count in meta when --include-count is set")
	}
	tcMap, ok := tc.(map[string]any)
	if !ok {
		t.Fatalf("expected total_count to be an object, got %T", tc)
	}
	if int(tcMap["value"].(float64)) != 5 {
		t.Errorf("expected total_count.value=5, got %v", tcMap["value"])
	}
}

// --- Edge cases ---

func TestCitiesMetricsCurrentNoResults(t *testing.T) {
	handler, _ := makeMetricsHandler("/cities", 0, false, 0, 10000)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "metrics", "current", "UNKNOWN_GEO",
		"--tag", "solar",
		"--property-type", "residential",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("expected 0 items, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 0 {
		t.Errorf("expected count=0, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if hasMore {
		t.Error("expected has_more=false for empty results")
	}
}

func TestCitiesMetricsMonthlyLimitAll(t *testing.T) {
	// 60 items fit in a single page (page max = 100).
	handler, queries := makeMetricsHandler("/cities", 60, true, 60, 9940)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "metrics", "monthly", "ABC123",
		"--tag", "solar",
		"--property-type", "residential",
		"--metric-from", "2020-01-01",
		"--metric-to", "2024-12-31",
		"--limit", "all",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 60 {
		t.Errorf("expected 60 items with --limit all, got %d", len(data))
	}

	// Should have made 1 request (all 60 items fit within page max of 100).
	if len(*queries) != 1 {
		t.Errorf("expected 1 API request for 60 items, got %d", len(*queries))
	}
}

// --- Error conditions ---

func TestCitiesMetricsCurrentMissingTag(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"cities", "metrics", "current", "ABC123",
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

func TestCitiesMetricsCurrentMissingPropertyType(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"cities", "metrics", "current", "ABC123",
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

func TestCitiesMetricsMonthlyMissingDateRange(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"cities", "metrics", "monthly", "ABC123",
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

func TestCitiesMetricsMonthlyInvalidDateFormat(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"cities", "metrics", "monthly", "ABC123",
		"--tag", "solar",
		"--property-type", "residential",
		"--metric-from", "01-01-2024",
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
	if !strings.Contains(p.Error, "invalid date format") {
		t.Errorf("expected error about invalid date format, got: %s", p.Error)
	}
	if !strings.Contains(p.Error, "YYYY-MM-DD") {
		t.Errorf("expected error to mention YYYY-MM-DD format, got: %s", p.Error)
	}
}

func TestCitiesMetricsCurrentRequiresAuth(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env,
		"cities", "metrics", "current", "ABC123",
		"--tag", "solar",
		"--property-type", "residential",
	)

	if result.ExitCode != 2 {
		t.Fatalf("expected exit 2, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "auth_error" {
		t.Errorf("expected error_type %q, got %q", "auth_error", p.ErrorType)
	}
}

func TestCitiesMetricsCurrentMissingGeoId(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"cities", "metrics", "current",
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
	if !strings.Contains(p.Error, "accepts 1 arg(s)") {
		t.Errorf("expected error about missing argument, got: %s", p.Error)
	}
}

func TestCitiesMetricsCurrentRejectsDateFlags(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"cities", "metrics", "current", "ABC123",
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
		t.Errorf("expected specific date flag rejection message, got: %s", p.Error)
	}
}

func TestCitiesMetricsCurrentRejectsMetricToFlag(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"cities", "metrics", "current", "ABC123",
		"--tag", "solar",
		"--property-type", "residential",
		"--metric-to", "2024-12-31",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "flags --metric-from/--metric-to are only valid on the monthly variant") {
		t.Errorf("expected specific date flag rejection message, got: %s", p.Error)
	}
}

// --- Boundary conditions ---

func TestCitiesMetricsHelpShowsSubcommands(t *testing.T) {
	result := runCLI(t, "cities", "metrics", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if !strings.Contains(result.Stdout, "current") {
		t.Error("cities metrics --help should list 'current' subcommand")
	}
	if !strings.Contains(result.Stdout, "monthly") {
		t.Error("cities metrics --help should list 'monthly' subcommand")
	}
}

func TestCitiesMetricsCurrentHelpContent(t *testing.T) {
	result := runCLI(t, "cities", "metrics", "current", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Documents that geo_id is positional.
	if !strings.Contains(result.Stdout, "GEO_ID") {
		t.Error("help should document GEO_ID as positional argument")
	}

	// Lists required flags.
	if !strings.Contains(result.Stdout, "--tag") {
		t.Error("help should list --tag flag")
	}
	if !strings.Contains(result.Stdout, "--property-type") {
		t.Error("help should list --property-type flag")
	}

	// Does NOT show --metric-from/--metric-to.
	if strings.Contains(result.Stdout, "--metric-from") {
		t.Error("current --help should NOT show --metric-from")
	}
	if strings.Contains(result.Stdout, "--metric-to") {
		t.Error("current --help should NOT show --metric-to")
	}

	// Shows workflow with cities search for geo_id resolution.
	if !strings.Contains(result.Stdout, "cities search") {
		t.Error("help should show workflow using cities search to resolve geo_id")
	}
}

func TestCitiesMetricsMonthlyHelpContent(t *testing.T) {
	result := runCLI(t, "cities", "metrics", "monthly", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Shows all required flags.
	requiredFlags := []string{"--tag", "--property-type", "--metric-from", "--metric-to"}
	for _, flag := range requiredFlags {
		if !strings.Contains(result.Stdout, flag) {
			t.Errorf("monthly --help should list %s flag", flag)
		}
	}

	// Documents GEO_ID as positional.
	if !strings.Contains(result.Stdout, "GEO_ID") {
		t.Error("help should document GEO_ID as positional argument")
	}
}

func TestCitiesMetricsCurrentAuthError(t *testing.T) {
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
		"cities", "metrics", "current", "ABC123",
		"--tag", "solar",
		"--property-type", "residential",
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
