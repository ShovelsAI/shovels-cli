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

// makeDecisionsSearchHandler returns an HTTP handler that validates the
// required decisions query parameters and serves paginated decision
// responses. totalItems controls the number of decisions across all pages.
func makeDecisionsSearchHandler(totalItems int, creditsUsed, creditsRemaining int) (http.Handler, *[]map[string][]string) {
	var served atomic.Int32
	capturedQueries := &[]map[string][]string{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/decisions/search" {
			w.WriteHeader(404)
			w.Write([]byte(`{"detail":"unexpected method or path"}`))
			return
		}

		params := map[string][]string{}
		for k, v := range r.URL.Query() {
			params[k] = v
		}
		*capturedQueries = append(*capturedQueries, params)

		if r.URL.Query().Get("geo_id") == "" {
			w.WriteHeader(422)
			w.Write([]byte(`{"detail":"geo_id is required"}`))
			return
		}
		if r.URL.Query().Get("decision_from") == "" {
			w.WriteHeader(422)
			w.Write([]byte(`{"detail":"decision_from is required"}`))
			return
		}
		if r.URL.Query().Get("decision_to") == "" {
			w.WriteHeader(422)
			w.Write([]byte(`{"detail":"decision_to is required"}`))
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

		items := make([]json.RawMessage, count)
		for i := range count {
			items[i] = json.RawMessage(fmt.Sprintf(
				`{"id":"d_%05d","decision_date":"2024-06-12","category":"Rezoning","zoning_new":"R3"}`,
				start+i,
			))
		}

		var nextCursor *string
		moreExist := (start + count) < totalItems
		if count > 0 && moreExist {
			c := fmt.Sprintf("cursor_%d", start+count)
			nextCursor = &c
		}

		w.Header().Set("X-Credits-Request", strconv.Itoa(creditsUsed))
		w.Header().Set("X-Credits-Remaining", strconv.Itoa(creditsRemaining))

		type totalCountShape struct {
			Value    int    `json:"value"`
			Relation string `json:"relation"`
		}
		type respShape struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
			TotalCount *totalCountShape  `json:"total_count,omitempty"`
		}
		resp := respShape{Items: items, NextCursor: nextCursor}

		if r.URL.Query().Get("include_count") == "true" && r.URL.Query().Get("cursor") == "" {
			resp.TotalCount = &totalCountShape{Value: totalItems, Relation: "eq"}
		}

		json.NewEncoder(w).Encode(resp)
	})

	return handler, capturedQueries
}

// --- Happy paths ---

func TestDecisionsSearchBasic(t *testing.T) {
	handler, queries := makeDecisionsSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 3 {
		t.Errorf("expected 3 items, got %d", len(data))
	}

	if int(parsed.Meta["count"].(float64)) != 3 {
		t.Errorf("expected count=3, got %v", parsed.Meta["count"])
	}

	if len(*queries) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*queries))
	}
	q := (*queries)[0]
	if q["geo_id"][0] != "CA" {
		t.Errorf("expected geo_id=CA, got %q", q["geo_id"])
	}
	if q["decision_from"][0] != "2024-01-01" {
		t.Errorf("expected decision_from=2024-01-01, got %q", q["decision_from"])
	}
	if q["decision_to"][0] != "2024-12-31" {
		t.Errorf("expected decision_to=2024-12-31, got %q", q["decision_to"])
	}
	if _, ok := q["size"]; !ok {
		t.Error("expected size param sent by paginator")
	}
}

func TestDecisionsSearchAllOptionalFiltersMapped(t *testing.T) {
	handler, queries := makeDecisionsSearchHandler(1, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--asset-class", "Residential",
		"--category", "Rezoning",
		"--subcategory", "X",
		"--property-type", "Y",
		"--min-project-value", "100000",
		"--max-project-value", "5000000",
		"--query", "downtown",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	checks := map[string]string{
		"asset_class":       "Residential",
		"category":          "Rezoning",
		"subcategory":       "X",
		"property_type":     "Y",
		"min_project_value": "100000",
		"max_project_value": "5000000",
		"decision_q":        "downtown",
	}
	for param, want := range checks {
		if len(q[param]) == 0 || q[param][0] != want {
			t.Errorf("expected %s=%q, got %v", param, want, q[param])
		}
	}
}

func TestDecisionsSearchRepeatableFilterMultipleFlags(t *testing.T) {
	handler, queries := makeDecisionsSearchHandler(1, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--asset-class", "A",
		"--asset-class", "B",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	q := (*queries)[0]
	vals := q["asset_class"]
	if len(vals) != 2 || vals[0] != "A" || vals[1] != "B" {
		t.Errorf("expected asset_class=[A B], got %v", vals)
	}
}

func TestDecisionsSearchRepeatableFilterCommaSeparated(t *testing.T) {
	handler, queries := makeDecisionsSearchHandler(1, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--category", "Rezoning,Variance",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	q := (*queries)[0]
	vals := q["category"]
	if len(vals) != 2 || vals[0] != "Rezoning" || vals[1] != "Variance" {
		t.Errorf("expected category=[Rezoning Variance], got %v", vals)
	}
}

func TestDecisionsSearchLimitAllPaginates(t *testing.T) {
	var requestCount atomic.Int32
	var badRoute atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/decisions/search" {
			badRoute.Store(true)
			w.WriteHeader(404)
			w.Write([]byte(`{"detail":"unexpected method or path"}`))
			return
		}
		n := int(requestCount.Add(1))
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		if size == 0 {
			size = 50
		}

		items := make([]json.RawMessage, size)
		for i := range size {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":"d_%05d"}`, (n-1)*size+i))
		}

		w.Header().Set("X-Credits-Request", "50")
		w.Header().Set("X-Credits-Remaining", "9800")

		var nextCursor *string
		// Serve two pages, then stop.
		if n < 2 {
			c := fmt.Sprintf("page%d", n+1)
			nextCursor = &c
		}
		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: nextCursor}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"--limit", "all",
		"--max-records", "1000",
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if badRoute.Load() {
		t.Fatal("expected all requests to GET /decisions/search")
	}

	parsed := parseEnvelope(t, result.Stdout)
	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected aggregated records across pages")
	}
	if requestCount.Load() < 2 {
		t.Errorf("expected pagination across at least 2 requests, got %d", requestCount.Load())
	}
}

func TestDecisionsSearchDryRun(t *testing.T) {
	// --dry-run must work without an API key and without calling the API.
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--asset-class", "Residential",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	if out.Method != "GET" {
		t.Errorf("expected method GET, got %q", out.Method)
	}
	if !strings.HasSuffix(out.URL, "/decisions/search") {
		t.Errorf("expected URL ending with /decisions/search, got %q", out.URL)
	}
	if out.Params["geo_id"] != "CA" {
		t.Errorf("expected geo_id=CA, got %v", out.Params["geo_id"])
	}
	if out.Params["decision_from"] != "2024-01-01" {
		t.Errorf("expected decision_from=2024-01-01, got %v", out.Params["decision_from"])
	}
	if out.Params["decision_to"] != "2024-12-31" {
		t.Errorf("expected decision_to=2024-12-31, got %v", out.Params["decision_to"])
	}
	ac, ok := out.Params["asset_class"].([]any)
	if !ok || len(ac) != 1 || ac[0] != "Residential" {
		t.Errorf("expected asset_class=[Residential], got %v", out.Params["asset_class"])
	}
}

// --- Edge cases ---

func TestDecisionsSearchOpaqueGeoIDAccepted(t *testing.T) {
	handler, queries := makeDecisionsSearchHandler(1, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "search",
		"--geo-id", "a4xysKbZwqg",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected opaque geo_id to be accepted, got exit %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	q := (*queries)[0]
	if q["geo_id"][0] != "a4xysKbZwqg" {
		t.Errorf("expected geo_id passed through unchanged, got %q", q["geo_id"])
	}
}

func TestDecisionsSearchStateCodeLowercasePassThrough(t *testing.T) {
	handler, queries := makeDecisionsSearchHandler(1, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "search",
		"--geo-id", "ca",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected lowercase state code to be accepted, got exit %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	q := (*queries)[0]
	if q["geo_id"][0] != "ca" {
		t.Errorf("expected geo_id passed through un-normalized as 'ca', got %q", q["geo_id"])
	}
}

func TestDecisionsSearchIncludeCount(t *testing.T) {
	// totalItems (120) exceeds the default page size (50), so `--limit all`
	// drives at least three cursor pages. This exercises the real contract:
	// total_count is captured from the FIRST page only, while include_count=true
	// must persist across every subsequent cursor-page request (no paginator
	// change drops the query params mid-walk).
	const totalItems = 120
	handler, queries := makeDecisionsSearchHandler(totalItems, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"--limit", "all",
		"--max-records", "1000",
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--include-count",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Must have genuinely crossed cursor pages.
	if len(*queries) < 2 {
		t.Fatalf("expected multiple cursor-page requests, got %d", len(*queries))
	}

	// (a) include_count=true is sent, AND (c) it persists on every page,
	// including those carrying a cursor.
	sawCursorPage := false
	for i, q := range *queries {
		ic := q["include_count"]
		if len(ic) != 1 || ic[0] != "true" {
			t.Errorf("page %d: expected include_count=[true] persisted, got %v", i, ic)
		}
		if len(q["cursor"]) > 0 && q["cursor"][0] != "" {
			sawCursorPage = true
		}
	}
	if !sawCursorPage {
		t.Fatal("expected at least one request to carry a cursor (second page)")
	}

	// First page must carry no cursor — that's where the count is captured.
	first := (*queries)[0]
	if len(first["cursor"]) > 0 && first["cursor"][0] != "" {
		t.Errorf("expected first request to have no cursor, got %v", first["cursor"])
	}

	// (b) The count is captured from the FIRST page's response and surfaced once
	// in meta, reflecting the full total rather than the per-page count.
	parsed := parseEnvelope(t, result.Stdout)
	tcVal, ok := parsed.Meta["total_count"]
	if !ok {
		t.Fatal("expected total_count in meta when --include-count is set")
	}
	tcMap := tcVal.(map[string]any)
	if int(tcMap["value"].(float64)) != totalItems {
		t.Errorf("expected total_count.value=%d captured from first page, got %v", totalItems, tcMap["value"])
	}
}

func TestDecisionsSearchQuery100RunesAccepted(t *testing.T) {
	handler, queries := makeDecisionsSearchHandler(1, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// 100 multi-byte runes: rune count is the limit, not bytes.
	exactQuery := strings.Repeat("é", 100)

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--query", exactQuery,
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 for 100-rune query, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	q := (*queries)[0]
	if q["decision_q"][0] != exactQuery {
		t.Errorf("expected decision_q to match the 100-rune query")
	}
}

func TestDecisionsSearchOptionalFlagsOmitted(t *testing.T) {
	handler, queries := makeDecisionsSearchHandler(2, 2, 9998)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	q := (*queries)[0]
	for _, optionalKey := range []string{
		"asset_class", "category", "subcategory", "property_type",
		"min_project_value", "max_project_value", "decision_q", "include_count",
	} {
		if _, exists := q[optionalKey]; exists {
			t.Errorf("optional param %q should not be sent when flag is omitted", optionalKey)
		}
	}
}

// --- Error conditions ---

func TestDecisionsSearchMissingAllRequiredFlags(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"decisions", "search",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	for _, flag := range []string{"--geo-id", "--decision-from", "--decision-to"} {
		if !strings.Contains(p.Error, flag) {
			t.Errorf("expected error to mention %s, got: %s", flag, p.Error)
		}
	}
}

func TestDecisionsSearchMissingOneRequiredFlag(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-to", "2024-12-31",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	if !strings.Contains(p.Error, "--decision-from") {
		t.Errorf("expected error to mention --decision-from, got: %s", p.Error)
	}
}

func TestDecisionsSearchBareZipRejected(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"decisions", "search",
		"--geo-id", "92024",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	if !strings.Contains(strings.ToLower(p.Error), "zip") {
		t.Errorf("expected ZIP-specific message, got: %s", p.Error)
	}
}

func TestDecisionsSearchZipPlusFourRejected(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"decisions", "search",
		"--geo-id", "92024-1234",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
}

func TestDecisionsSearchPrefixedGeoIDRejected(t *testing.T) {
	for _, geoID := range []string{"ZIP_90210", "CITY_LOS_ANGELES_CA", "COUNTY_X", "STATE_CA", "ADDR_1"} {
		env := withIsolatedConfig(t)
		result := runCLIWithEnv(t, env,
			"decisions", "search",
			"--geo-id", geoID,
			"--decision-from", "2024-01-01",
			"--decision-to", "2024-12-31",
		)

		if result.ExitCode != 1 {
			t.Fatalf("geo_id %q: expected exit 1, got %d; stderr: %s", geoID, result.ExitCode, result.Stderr)
		}
		p := parseStderrError(t, result.Stderr)
		if p.ErrorType != "validation_error" {
			t.Errorf("geo_id %q: expected error_type validation_error, got %q", geoID, p.ErrorType)
		}
	}
}

func TestDecisionsSearchInvalidDateFormat(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024/01/01",
		"--decision-to", "2024-12-31",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	if !strings.Contains(p.Error, "--decision-from") {
		t.Errorf("expected error to mention --decision-from, got: %s", p.Error)
	}
}

func TestDecisionsSearchQueryTooLong(t *testing.T) {
	env := withIsolatedConfig(t)
	longQuery := strings.Repeat("a", 101)

	result := runCLIWithEnv(t, env,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--query", longQuery,
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	if !strings.Contains(p.Error, "100") {
		t.Errorf("expected error to mention the 100-char limit, got: %s", p.Error)
	}
}

func TestDecisionsSearchNegativeMinProjectValue(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--min-project-value", "-1",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
}

func TestDecisionsSearchNegativeMaxProjectValue(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--max-project-value", "-1",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
}

func TestDecisionsSearchRequiresAuth(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
	)

	if result.ExitCode != 2 {
		t.Fatalf("expected exit 2 with no API key, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "auth_error" {
		t.Errorf("expected error_type auth_error, got %q", p.ErrorType)
	}
}

func TestDecisionsSearchAPIErrorExitCodeMapping(t *testing.T) {
	// API error responses must map to project exit codes via shared
	// client.APIError handling: auth=2, rate-limit=3, credit-exhausted=4,
	// server=5.
	cases := []struct {
		status   int
		exitCode int
		errType  string
	}{
		{401, 2, "auth_error"},
		{429, 3, "rate_limited"},
		{402, 4, "credit_exhausted"},
		{500, 5, "server_error"},
		{503, 5, "server_error"},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(makeStatusHandler(tc.status))
			defer srv.Close()

			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env,
				"--base-url", srv.URL,
				"--no-retry",
				"decisions", "search",
				"--geo-id", "CA",
				"--decision-from", "2024-01-01",
				"--decision-to", "2024-12-31",
			)
			if result.ExitCode != tc.exitCode {
				t.Fatalf("status %d: expected exit %d, got %d; stderr: %s", tc.status, tc.exitCode, result.ExitCode, result.Stderr)
			}
			p := parseStderrError(t, result.Stderr)
			if p.ErrorType != tc.errType {
				t.Errorf("status %d: expected error_type %q, got %q", tc.status, tc.errType, p.ErrorType)
			}
		})
	}
}

// --- Boundary conditions ---

func TestDecisionsSearchSingleDayRange(t *testing.T) {
	handler, queries := makeDecisionsSearchHandler(1, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-06-12",
		"--decision-to", "2024-06-12",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected single-day range to be accepted, got exit %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	q := (*queries)[0]
	if q["decision_from"][0] != "2024-06-12" || q["decision_to"][0] != "2024-06-12" {
		t.Errorf("expected single-day range forwarded, got from=%v to=%v", q["decision_from"], q["decision_to"])
	}
}

func TestDecisionsSearchReversedDateRangeForwarded(t *testing.T) {
	// from > to is enforced server-side, not client-side: the CLI must FORWARD
	// the reversed range to the API and surface the resulting 422. A local
	// from>to rejection would also exit 1 with a validation_error type, so exit
	// code alone cannot distinguish the two. This test proves forwarding by
	// requiring (a) the API handler was actually invoked with the reversed
	// params, and (b) the surfaced error carries the API's own 422 message.
	var apiCalled atomic.Bool
	var gotFrom, gotTo atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/decisions/search" {
			w.WriteHeader(404)
			w.Write([]byte(`{"detail":"unexpected method or path"}`))
			return
		}
		apiCalled.Store(true)
		from := r.URL.Query().Get("decision_from")
		to := r.URL.Query().Get("decision_to")
		gotFrom.Store(from)
		gotTo.Store(to)
		if from > to {
			w.WriteHeader(422)
			w.Write([]byte(`{"detail":"decision_from must be <= decision_to"}`))
			return
		}
		w.Header().Set("X-Credits-Request", "0")
		w.Header().Set("X-Credits-Remaining", "9999")
		w.Write([]byte(`{"items":[],"next_cursor":null}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"--no-retry",
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-12-31",
		"--decision-to", "2024-01-01",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1 from API 422, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// The request must have reached the API — a client-side from>to rejection
	// would never call the server.
	if !apiCalled.Load() {
		t.Fatal("reversed range was rejected client-side: API handler was never invoked")
	}
	if f, _ := gotFrom.Load().(string); f != "2024-12-31" {
		t.Errorf("expected reversed decision_from=2024-12-31 forwarded, got %q", f)
	}
	if to, _ := gotTo.Load().(string); to != "2024-01-01" {
		t.Errorf("expected reversed decision_to=2024-01-01 forwarded, got %q", to)
	}

	// The surfaced error must be the API's 422 message, not a local one.
	p := parseStderrError(t, result.Stderr)
	if !strings.Contains(p.Error, "decision_from must be <= decision_to") {
		t.Errorf("expected the API 422 message to be surfaced, got: %s", p.Error)
	}
}

func TestDecisionsSearchMinProjectValueZeroAccepted(t *testing.T) {
	handler, queries := makeDecisionsSearchHandler(1, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "search",
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--min-project-value", "0",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected --min-project-value 0 to be accepted, got exit %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	q := (*queries)[0]
	if len(q["min_project_value"]) == 0 || q["min_project_value"][0] != "0" {
		t.Errorf("expected min_project_value=0 forwarded, got %v", q["min_project_value"])
	}
}

// =======================================================================
// decisions get
// =======================================================================

// makeDecisionsGetHandler returns an HTTP handler that serves batch decision
// responses. It rejects any request that is not GET /decisions, so a wrong
// verb or path fails the test. knownIDs defines which IDs exist; unknown IDs
// are omitted from the response (the caller detects them as missing).
func makeDecisionsGetHandler(knownIDs map[string]bool, creditsUsed, creditsRemaining int) (http.Handler, *[]string) {
	captured := &[]string{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/decisions" {
			w.WriteHeader(404)
			w.Write([]byte(`{"detail":"unexpected method or path"}`))
			return
		}

		ids := r.URL.Query()["id"]
		*captured = append(*captured, ids...)

		var items []json.RawMessage
		for _, id := range ids {
			if knownIDs[id] {
				items = append(items, json.RawMessage(fmt.Sprintf(
					`{"id":%q,"decision_date":"2024-06-12","category":"Rezoning","zoning_new":"R3"}`, id,
				)))
			}
		}
		if items == nil {
			items = []json.RawMessage{}
		}

		w.Header().Set("X-Credits-Request", strconv.Itoa(creditsUsed))
		w.Header().Set("X-Credits-Remaining", strconv.Itoa(creditsRemaining))

		resp := struct {
			Items []json.RawMessage `json:"items"`
		}{Items: items}
		json.NewEncoder(w).Encode(resp)
	})
	return handler, captured
}

// --- decisions get: Happy paths ---

func TestDecisionsGetMultipleIDs(t *testing.T) {
	known := map[string]bool{"d_abc": true, "d_def": true}
	handler, captured := makeDecisionsGetHandler(known, 2, 9998)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "get", "d_abc", "d_def",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 2 {
		t.Errorf("expected 2 items, got %d", len(data))
	}

	if int(parsed.Meta["count"].(float64)) != 2 {
		t.Errorf("expected count=2, got %v", parsed.Meta["count"])
	}

	// meta.missing should be absent when all IDs found.
	if _, ok := parsed.Meta["missing"]; ok {
		t.Error("expected missing to be absent when all IDs found")
	}

	// PrintBatch convention: no has_more for non-paginated batch responses.
	if _, ok := parsed.Meta["has_more"]; ok {
		t.Error("batch response should not have has_more in meta")
	}

	if int(parsed.Meta["credits_used"].(float64)) != 2 {
		t.Errorf("expected credits_used=2, got %v", parsed.Meta["credits_used"])
	}
	if int(parsed.Meta["credits_remaining"].(float64)) != 9998 {
		t.Errorf("expected credits_remaining=9998, got %v", parsed.Meta["credits_remaining"])
	}

	// Both IDs must have been forwarded as repeated id params.
	if len(*captured) != 2 || (*captured)[0] != "d_abc" || (*captured)[1] != "d_def" {
		t.Errorf("expected id params [d_abc d_def] forwarded, got %v", *captured)
	}
}

func TestDecisionsGetSomeMissing(t *testing.T) {
	known := map[string]bool{"d_abc": true}
	handler, _ := makeDecisionsGetHandler(known, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "get", "d_abc", "d_missing", "d_gone",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 1 {
		t.Errorf("expected 1 item (only d_abc found), got %d", len(data))
	}

	if int(parsed.Meta["count"].(float64)) != 1 {
		t.Errorf("expected count=1, got %v", parsed.Meta["count"])
	}

	missingVal, ok := parsed.Meta["missing"]
	if !ok {
		t.Fatal("expected missing in meta when some IDs not found")
	}
	missingArr, ok := missingVal.([]any)
	if !ok {
		t.Fatalf("expected missing to be array, got %T", missingVal)
	}
	if len(missingArr) != 2 {
		t.Fatalf("expected 2 missing IDs, got %d", len(missingArr))
	}
	if missingArr[0].(string) != "d_missing" {
		t.Errorf("expected first missing ID d_missing, got %q", missingArr[0])
	}
	if missingArr[1].(string) != "d_gone" {
		t.Errorf("expected second missing ID d_gone, got %q", missingArr[1])
	}
}

func TestDecisionsGetDryRun(t *testing.T) {
	// --dry-run must work without an API key and without calling the API.
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env,
		"decisions", "get", "d_abc", "d_def",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	if out.Method != "GET" {
		t.Errorf("expected method GET, got %q", out.Method)
	}
	if !strings.HasSuffix(out.URL, "/decisions") {
		t.Errorf("expected URL ending with /decisions, got %q", out.URL)
	}
	ids, ok := out.Params["id"].([]any)
	if !ok || len(ids) != 2 || ids[0] != "d_abc" || ids[1] != "d_def" {
		t.Errorf("expected id=[d_abc d_def], got %v", out.Params["id"])
	}
}

// --- decisions get: Edge cases ---

func TestDecisionsGetSingleID(t *testing.T) {
	known := map[string]bool{"d_abc": true}
	handler, _ := makeDecisionsGetHandler(known, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "get", "d_abc",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 1 {
		t.Errorf("expected 1 item, got %d", len(data))
	}
	if int(parsed.Meta["count"].(float64)) != 1 {
		t.Errorf("expected count=1, got %v", parsed.Meta["count"])
	}
	if _, ok := parsed.Meta["missing"]; ok {
		t.Error("expected missing to be absent when all IDs found")
	}
}

func TestDecisionsGetDuplicateUnknownIDs(t *testing.T) {
	// Duplicate IDs pass through as-is; a duplicated unknown ID may appear
	// more than once in meta.missing (matches findMissingIDs/permits behavior).
	handler, captured := makeDecisionsGetHandler(map[string]bool{}, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"decisions", "get", "d_missing", "d_missing",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Both duplicates must reach the API unchanged.
	if len(*captured) != 2 || (*captured)[0] != "d_missing" || (*captured)[1] != "d_missing" {
		t.Errorf("expected both duplicate ids forwarded, got %v", *captured)
	}

	parsed := parseEnvelope(t, result.Stdout)
	missingArr, ok := parsed.Meta["missing"].([]any)
	if !ok {
		t.Fatalf("expected missing array, got %T", parsed.Meta["missing"])
	}
	if len(missingArr) != 2 {
		t.Errorf("expected duplicated unknown ID to appear twice in missing, got %v", missingArr)
	}
}

// --- decisions get: Error conditions ---

func TestDecisionsGetNoIDs(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"decisions", "get",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	if p.Error != "at least one decision ID required" {
		t.Errorf("expected error %q, got %q", "at least one decision ID required", p.Error)
	}
}

func TestDecisionsGetTooManyIDs(t *testing.T) {
	env := withIsolatedConfig(t)

	args := []string{"decisions", "get"}
	for i := range 51 {
		args = append(args, fmt.Sprintf("d_%05d", i))
	}

	result := runCLIWithEnv(t, env, args...)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	if p.Error != "maximum 50 IDs per request" {
		t.Errorf("expected error %q, got %q", "maximum 50 IDs per request", p.Error)
	}
}

func TestDecisionsGetIDAsFlagRejected(t *testing.T) {
	// An ID passed as --id flag yields cobra's unknown-flag error (exit 1);
	// help text steers users to positional usage.
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"decisions", "get", "--id", "d_abc",
	)

	if result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit for unknown --id flag, got 0; stdout: %s", result.Stdout)
	}
	if !strings.Contains(result.Stderr, "unknown flag") {
		t.Errorf("expected unknown-flag error, got: %s", result.Stderr)
	}
}

func TestDecisionsGetAPIErrorExitCodeMapping(t *testing.T) {
	// API error responses must map to project exit codes via shared
	// client.APIError handling: auth=2, rate-limit=3, credit-exhausted=4,
	// server=5.
	cases := []struct {
		status   int
		exitCode int
		errType  string
	}{
		{401, 2, "auth_error"},
		{429, 3, "rate_limited"},
		{402, 4, "credit_exhausted"},
		{500, 5, "server_error"},
		{503, 5, "server_error"},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(makeStatusHandler(tc.status))
			defer srv.Close()

			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env,
				"--base-url", srv.URL,
				"--no-retry",
				"decisions", "get", "d_abc",
			)
			if result.ExitCode != tc.exitCode {
				t.Fatalf("status %d: expected exit %d, got %d; stderr: %s", tc.status, tc.exitCode, result.ExitCode, result.Stderr)
			}
			p := parseStderrError(t, result.Stderr)
			if p.ErrorType != tc.errType {
				t.Errorf("status %d: expected error_type %q, got %q", tc.status, tc.errType, p.ErrorType)
			}
		})
	}
}

// --- decisions get: Boundary conditions ---

func TestDecisionsGetExactly50IDs(t *testing.T) {
	known := make(map[string]bool, 50)
	for i := range 50 {
		known[fmt.Sprintf("d_%05d", i)] = true
	}

	handler, captured := makeDecisionsGetHandler(known, 50, 9950)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	args := []string{"--base-url", srv.URL, "decisions", "get"}
	for i := range 50 {
		args = append(args, fmt.Sprintf("d_%05d", i))
	}

	result := runCLIWithEnv(t, env, args...)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 for exactly 50 IDs, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 50 {
		t.Errorf("expected 50 items, got %d", len(data))
	}
	if int(parsed.Meta["count"].(float64)) != 50 {
		t.Errorf("expected count=50, got %v", parsed.Meta["count"])
	}
	if _, ok := parsed.Meta["missing"]; ok {
		t.Error("expected missing to be absent when all 50 IDs found")
	}
	if len(*captured) != 50 {
		t.Errorf("expected 50 id params forwarded, got %d", len(*captured))
	}
}
