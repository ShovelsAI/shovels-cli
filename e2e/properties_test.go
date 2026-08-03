//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// trustObject is a representative per-row PropertyTrust value as the API emits
// it on absence-class rows.
const trustObject = `{"unresolved_rate":0.0714,"coverage_tier":"high","data_horizon":"2025-09-30","horizon_basis":"measured","trust_jurisdiction_basis":"own","trust_jurisdiction_error_bar":0.0,"footprint_basis":"matched","flags":[]}`

// propertiesPage describes one mock /properties/search page. Its raw-JSON
// fields are held as strings so a fixture can express the three states the
// paginator distinguishes: "" omits the field entirely, "null" emits an
// explicit null, and anything else is the literal value. An empty nextCursor
// ends the chain; totalCount is emitted only when the request asked for a
// count.
type propertiesPage struct {
	rows         []json.RawMessage
	nextCursor   string
	trustSummary string
	totalCount   string
}

func (p propertiesPage) body(includeTotalCount bool) []byte {
	// omitempty on the raw fields is what turns an unset fixture field into
	// an absent JSON key.
	type pageBody struct {
		Items        []json.RawMessage `json:"items"`
		Size         int               `json:"size"`
		NextCursor   *string           `json:"next_cursor"`
		TrustSummary json.RawMessage   `json:"trust_summary,omitempty"`
		TotalCount   json.RawMessage   `json:"total_count,omitempty"`
	}

	items := p.rows
	if items == nil {
		items = []json.RawMessage{}
	}
	shape := pageBody{Items: items, Size: len(items)}
	if p.nextCursor != "" {
		shape.NextCursor = &p.nextCursor
	}
	if p.trustSummary != "" {
		shape.TrustSummary = json.RawMessage(p.trustSummary)
	}
	if includeTotalCount && p.totalCount != "" {
		shape.TotalCount = json.RawMessage(p.totalCount)
	}

	encoded, err := json.Marshal(shape)
	if err != nil {
		panic(fmt.Sprintf("properties page fixture is not marshalable: %v", err))
	}
	return encoded
}

// propertyRows builds n property rows starting at the given id offset, each
// carrying the raw trust JSON when it is non-empty.
func propertyRows(offset, n int, trust string) []json.RawMessage {
	rows := make([]json.RawMessage, n)
	for i := range n {
		trustField := ""
		if trust != "" {
			trustField = `,"trust":` + trust
		}
		rows[i] = json.RawMessage(fmt.Sprintf(
			`{"id":"a_%05d","street":"MAIN ST","tags":["roofing"]%s}`,
			offset+i, trustField,
		))
	}
	return rows
}

// makePropertiesSearchHandler serves the given pages in request order,
// enforces the endpoint's scope requirement, and records every request's
// query parameters.
func makePropertiesSearchHandler(pages []propertiesPage) (http.Handler, *[]map[string][]string) {
	var served atomic.Int32
	captured := &[]map[string][]string{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/properties/search" {
			w.WriteHeader(404)
			w.Write([]byte(`{"detail":"unexpected method or path"}`))
			return
		}

		params := map[string][]string{}
		for k, v := range r.URL.Query() {
			params[k] = v
		}
		*captured = append(*captured, params)

		if r.URL.Query().Get("geo_id") == "" && len(r.URL.Query()["legal_owner"]) == 0 {
			w.WriteHeader(422)
			w.Write([]byte(`{"detail":"at least one of geo_id and legal_owner is required"}`))
			return
		}

		idx := int(served.Add(1)) - 1
		if idx >= len(pages) {
			w.WriteHeader(500)
			w.Write([]byte(`{"detail":"more pages requested than the fixture provides"}`))
			return
		}

		w.Header().Set("X-Credits-Request", "1")
		w.Header().Set("X-Credits-Remaining", "9999")
		wantCount := r.URL.Query().Get("include_total_count") == "true" && r.URL.Query().Get("cursor") == ""
		w.Write(pages[idx].body(wantCount))
	})

	return handler, captured
}

// singlePropertiesPage is the common fixture: one page of n rows, no cursor.
func singlePropertiesPage(n int) []propertiesPage {
	return []propertiesPage{{rows: propertyRows(0, n, "")}}
}

// decodeJSONValue parses raw JSON with UseNumber so numeric literals keep
// their exact textual form and a float64 round-trip cannot mask a
// representation change.
func decodeJSONValue(t *testing.T, raw string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("invalid JSON %q: %v", raw, err)
	}
	return v
}

// assertJSONValueEqual fails unless two JSON documents parse to the same value.
func assertJSONValueEqual(t *testing.T, label, want, got string) {
	t.Helper()
	if !reflect.DeepEqual(decodeJSONValue(t, want), decodeJSONValue(t, got)) {
		t.Errorf("%s changed in transit:\n want %s\n got  %s", label, want, got)
	}
}

// dataRows unmarshals the envelope's data array into raw rows.
func dataRows(t *testing.T, parsed envelope) []json.RawMessage {
	t.Helper()
	var rows []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &rows); err != nil {
		t.Fatalf("expected data to be an array: %v", err)
	}
	return rows
}

// metaTrustSummaries returns the raw meta.trust_summaries entries, failing if
// the key is absent.
func metaTrustSummaries(t *testing.T, stdout string) []json.RawMessage {
	t.Helper()
	var env struct {
		Meta struct {
			TrustSummaries []json.RawMessage `json:"trust_summaries"`
		} `json:"meta"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdout)
	}
	if env.Meta.TrustSummaries == nil {
		t.Fatalf("expected meta.trust_summaries in output, got: %s", stdout)
	}
	return env.Meta.TrustSummaries
}

// --- Happy paths ---

func TestPropertiesSearchBasic(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(3))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "CA",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	if got := len(dataRows(t, parsed)); got != 3 {
		t.Errorf("expected 3 rows, got %d", got)
	}
	if int(parsed.Meta["count"].(float64)) != 3 {
		t.Errorf("expected count=3, got %v", parsed.Meta["count"])
	}
	if parsed.Meta["has_more"] != false {
		t.Errorf("expected has_more=false, got %v", parsed.Meta["has_more"])
	}

	if len(*queries) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*queries))
	}
	q := (*queries)[0]
	if q["geo_id"][0] != "CA" {
		t.Errorf("expected geo_id=CA, got %v", q["geo_id"])
	}
	if got := q["size"]; len(got) != 1 || got[0] != "50" {
		t.Errorf("expected the default --limit 50 to request size=50, got %v", got)
	}
}

func TestPropertiesSearchLimitAllPaginates(t *testing.T) {
	handler, queries := makePropertiesSearchHandler([]propertiesPage{
		{rows: propertyRows(0, 100, ""), nextCursor: "c1"},
		{rows: propertyRows(100, 40, "")},
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "CA",
		"--limit", "all",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*queries) != 2 {
		t.Fatalf("expected the cursor chain to be followed over 2 pages, got %d requests", len(*queries))
	}
	if got := (*queries)[1]["cursor"]; len(got) != 1 || got[0] != "c1" {
		t.Errorf("expected the second request to carry cursor=c1, got %v", got)
	}

	parsed := parseEnvelope(t, result.Stdout)
	if got := len(dataRows(t, parsed)); got != 140 {
		t.Errorf("expected 140 rows merged across pages, got %d", got)
	}
	if parsed.Meta["has_more"] != false {
		t.Errorf("expected has_more=false once the cursor chain ends, got %v", parsed.Meta["has_more"])
	}
}

func TestPropertiesSearchLegalOwnerOnlyIsNationwide(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(2))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--legal-owner", "INVITATION HOMES",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	q := (*queries)[0]
	if _, ok := q["geo_id"]; ok {
		t.Errorf("expected no geo_id param for a nationwide owner search, got %v", q["geo_id"])
	}
	if vals := q["legal_owner"]; len(vals) != 1 || vals[0] != "INVITATION HOMES" {
		t.Errorf("expected legal_owner=[INVITATION HOMES], got %v", vals)
	}
	if _, ok := q["size"]; !ok {
		t.Error("expected the paginator's size param even without a geo scope")
	}
}

func TestPropertiesSearchLegalOwnerCommasNotSplit(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--legal-owner", "SMITH, JOHN",
		"--legal-owner", "ACME LLC",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	vals := (*queries)[0]["legal_owner"]
	if len(vals) != 2 {
		t.Fatalf("expected exactly 2 legal_owner params, got %v", vals)
	}
	if vals[0] != "SMITH, JOHN" || vals[1] != "ACME LLC" {
		t.Errorf("expected owner values byte-identical to the flags, got %v", vals)
	}
}

func TestPropertiesSearchGeoIDAndLegalOwnerCombined(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "92024",
		"--legal-owner", "ACME LLC",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	q := (*queries)[0]
	if len(q["geo_id"]) != 1 || q["geo_id"][0] != "92024" {
		t.Errorf("expected geo_id=[92024], got %v", q["geo_id"])
	}
	if len(q["legal_owner"]) != 1 || q["legal_owner"][0] != "ACME LLC" {
		t.Errorf("expected legal_owner=[ACME LLC], got %v", q["legal_owner"])
	}
}

func TestPropertiesSearchPermitFiltersMapped(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "CA",
		"--permit-tags", "solar,-roofing",
		"--permit-status", "final,active",
		"--permit-from", "2024-01-01",
		"--permit-tags-unfinaled", "solar",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	q := (*queries)[0]
	checks := map[string]string{
		"permit_tags":           "solar,-roofing",
		"permit_status":         "final,active",
		"permit_from":           "2024-01-01",
		"permit_tags_unfinaled": "solar",
	}
	for param, want := range checks {
		if len(q[param]) != 1 || q[param][0] != want {
			t.Errorf("expected %s=[%q], got %v", param, want, q[param])
		}
	}
	if _, ok := q["permit_to"]; ok {
		t.Errorf("permit_to must never be sent, got %v", q["permit_to"])
	}
}

func TestPropertiesSearchIncludeCount(t *testing.T) {
	handler, queries := makePropertiesSearchHandler([]propertiesPage{{
		rows:       propertyRows(0, 2, ""),
		totalCount: `{"value":4200,"relation":"eq"}`,
	}})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "CA",
		"--include-count",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if got := (*queries)[0]["include_total_count"]; len(got) != 1 || got[0] != "true" {
		t.Errorf("expected include_total_count=[true], got %v", got)
	}

	parsed := parseEnvelope(t, result.Stdout)
	tc, ok := parsed.Meta["total_count"].(map[string]any)
	if !ok {
		t.Fatalf("expected meta.total_count object, got %v", parsed.Meta["total_count"])
	}
	if int(tc["value"].(float64)) != 4200 || tc["relation"] != "eq" {
		t.Errorf("expected total_count {value:4200, relation:eq}, got %v", tc)
	}
}

func TestPropertiesSearchNullTotalCountOmitted(t *testing.T) {
	// A timed-out count query returns total_count: null, which must not
	// surface as a meta key.
	handler, _ := makePropertiesSearchHandler([]propertiesPage{{
		rows:       propertyRows(0, 2, ""),
		totalCount: `null`,
	}})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "CA",
		"--include-count",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	if _, ok := parsed.Meta["total_count"]; ok {
		t.Errorf("expected meta.total_count to be omitted when the count is null, got %v", parsed.Meta["total_count"])
	}
}

func TestPropertiesSearchDryRun(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"--dry-run",
		"properties", "search",
		"--geo-id", "92024",
		"--permit-tags", "-solar",
		"--limit", "10",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(*queries) != 0 {
		t.Errorf("expected zero API requests on --dry-run, got %d", len(*queries))
	}

	var req struct {
		Method string         `json:"method"`
		URL    string         `json:"url"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &req); err != nil {
		t.Fatalf("dry-run stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if req.Method != "GET" {
		t.Errorf("expected method GET, got %q", req.Method)
	}
	if !strings.HasSuffix(req.URL, "/properties/search") {
		t.Errorf("expected URL to end in /properties/search, got %q", req.URL)
	}
	if req.Params["geo_id"] != "92024" {
		t.Errorf("expected geo_id=92024 in dry-run params, got %v", req.Params["geo_id"])
	}
	if req.Params["permit_tags"] == nil {
		t.Errorf("expected permit_tags in dry-run params, got %v", req.Params)
	}
}

func TestPropertiesSearchRowTrustPassthrough(t *testing.T) {
	handler, _ := makePropertiesSearchHandler([]propertiesPage{{
		rows:         propertyRows(0, 2, trustObject),
		trustSummary: `{"rows_flagged":0,"row_weighted_unresolved_rate":0.0714,"expected_miss_rate":0.031,"suppressed_scopes":0}`,
	}})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "92024",
		"--permit-tags", "-solar",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	rows := dataRows(t, parseEnvelope(t, result.Stdout))
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	for i, row := range rows {
		var parsedRow struct {
			Trust json.RawMessage `json:"trust"`
		}
		if err := json.Unmarshal(row, &parsedRow); err != nil {
			t.Fatalf("row %d is not an object: %v", i, err)
		}
		if len(parsedRow.Trust) == 0 {
			t.Fatalf("row %d lost its trust object: %s", i, row)
		}
		assertJSONValueEqual(t, fmt.Sprintf("row %d trust", i), trustObject, string(parsedRow.Trust))
	}
}

func TestPropertiesSearchTrustSummarySinglePage(t *testing.T) {
	const summary = `{"rows_flagged":3,"row_weighted_unresolved_rate":0.0714,"expected_miss_rate":0.031,"suppressed_scopes":0}`
	handler, _ := makePropertiesSearchHandler([]propertiesPage{{
		rows:         propertyRows(0, 2, trustObject),
		trustSummary: summary,
	}})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "92024",
		"--permit-tags", "-solar",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	summaries := metaTrustSummaries(t, result.Stdout)
	if len(summaries) != 1 {
		t.Fatalf("expected a single-entry trust_summaries array, got %d entries", len(summaries))
	}
	assertJSONValueEqual(t, "trust_summaries[0]", summary, string(summaries[0]))
}

func TestPropertiesSearchTrustSummariesSkipNullPages(t *testing.T) {
	// Pages without a summary contribute no entry, so the array collects only
	// non-null summaries and is not page-index aligned.
	const summary = `{"rows_flagged":9,"row_weighted_unresolved_rate":0.2,"expected_miss_rate":0.05,"suppressed_scopes":1}`
	handler, _ := makePropertiesSearchHandler([]propertiesPage{
		{rows: propertyRows(0, 100, trustObject), nextCursor: "c1", trustSummary: "null"},
		{rows: propertyRows(100, 100, trustObject), nextCursor: "c2", trustSummary: summary},
		{rows: propertyRows(200, 20, trustObject)},
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "92024",
		"--permit-tags", "-solar",
		"--limit", "220",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	summaries := metaTrustSummaries(t, result.Stdout)
	if len(summaries) != 1 {
		t.Fatalf("expected only the one non-null summary, got %d entries: %v", len(summaries), summaries)
	}
	assertJSONValueEqual(t, "trust_summaries[0]", summary, string(summaries[0]))
}

func TestPropertiesSearchTrustSummariesAbsentWhenNoPageCarriesOne(t *testing.T) {
	handler, _ := makePropertiesSearchHandler(singlePropertiesPage(2))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "92024",
		"--permit-tags", "solar",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	if _, ok := parsed.Meta["trust_summaries"]; ok {
		t.Errorf("expected meta.trust_summaries to be absent on a presence-only search, got %v", parsed.Meta["trust_summaries"])
	}
}

// --- Edge cases ---

func TestPropertiesSearchZipScopesAccepted(t *testing.T) {
	// Properties accepts ZIP and ZIP+4 scopes directly, diverging from
	// decisions, which rejects them client-side.
	for _, geoID := range []string{"92024", "92024-1234"} {
		t.Run(geoID, func(t *testing.T) {
			handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
			srv := httptest.NewServer(handler)
			defer srv.Close()

			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env,
				"--base-url", srv.URL,
				"properties", "search",
				"--geo-id", geoID,
			)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}
			if len(*queries) != 1 {
				t.Fatalf("expected the ZIP scope to reach the API, got %d requests", len(*queries))
			}
			if got := (*queries)[0]["geo_id"]; len(got) != 1 || got[0] != geoID {
				t.Errorf("expected geo_id=[%q], got %v", geoID, got)
			}
		})
	}
}

func TestPropertiesSearchJurisdictionGeoIDRejectedByServer(t *testing.T) {
	// Jurisdiction ids are opaque, so only the server can reject them; the
	// CLI must forward the request and surface the API's 422.
	var queries []map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := map[string][]string{}
		for k, v := range r.URL.Query() {
			params[k] = v
		}
		queries = append(queries, params)
		w.WriteHeader(422)
		w.Write([]byte(`{"detail":"jurisdiction geolocation ids are not accepted"}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "a4xysKbZwqg",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1 for a server-side 422, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(queries) != 1 {
		t.Fatalf("expected the opaque geo_id to reach the API, got %d requests", len(queries))
	}
	if got := queries[0]["geo_id"]; len(got) != 1 || got[0] != "a4xysKbZwqg" {
		t.Errorf("expected geo_id=[a4xysKbZwqg] forwarded unchanged, got %v", got)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	if !strings.Contains(p.Error, "jurisdiction") {
		t.Errorf("expected the API's own message to be surfaced, got %q", p.Error)
	}
}

func TestPropertiesSearchTrustSummariesOnePerFetchedPage(t *testing.T) {
	summaries := []string{
		`{"rows_flagged":1,"row_weighted_unresolved_rate":0.01,"expected_miss_rate":0.011,"suppressed_scopes":0}`,
		`{"rows_flagged":2,"row_weighted_unresolved_rate":0.02,"expected_miss_rate":0.022,"suppressed_scopes":0}`,
		`{"rows_flagged":3,"row_weighted_unresolved_rate":0.03,"expected_miss_rate":0.033,"suppressed_scopes":0}`,
	}
	handler, queries := makePropertiesSearchHandler([]propertiesPage{
		{rows: propertyRows(0, 100, trustObject), nextCursor: "c1", trustSummary: summaries[0]},
		{rows: propertyRows(100, 100, trustObject), nextCursor: "c2", trustSummary: summaries[1]},
		{rows: propertyRows(200, 50, trustObject), trustSummary: summaries[2]},
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "CA",
		"--permit-tags", "-solar",
		"--limit", "250",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(*queries) != 3 {
		t.Fatalf("expected 3 API pages, got %d", len(*queries))
	}

	got := metaTrustSummaries(t, result.Stdout)
	if len(got) != 3 {
		t.Fatalf("expected 3 trust summaries, got %d", len(got))
	}
	for i, want := range summaries {
		assertJSONValueEqual(t, fmt.Sprintf("trust_summaries[%d]", i), want, string(got[i]))
	}
}

// --- Error conditions ---

func TestPropertiesSearchMissingScope(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(*queries) != 0 {
		t.Errorf("expected zero API requests, got %d", len(*queries))
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	if !strings.Contains(p.Error, "--geo-id") || !strings.Contains(p.Error, "--legal-owner") {
		t.Errorf("expected the message to name both scope flags, got %q", p.Error)
	}
}

func TestPropertiesSearchEmptyGeoIDAloneRejected(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected an empty --geo-id to be treated as absent, got exit %d", result.ExitCode)
	}
	if len(*queries) != 0 {
		t.Errorf("expected zero API requests, got %d", len(*queries))
	}
}

func TestPropertiesSearchSoleEmptyLegalOwnerIsMissingScope(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--legal-owner", "",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1 for a sole empty owner, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(*queries) != 0 {
		t.Errorf("expected zero API requests, got %d", len(*queries))
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	// An owner list of nothing but empty strings leaves the search unscoped,
	// so the missing-scope message wins over the bad-value one. Naming
	// --geo-id is the discriminator: the bad-value message never does.
	if !strings.Contains(p.Error, "--geo-id") || !strings.Contains(p.Error, "--legal-owner") {
		t.Errorf("expected the missing-scope message naming both scope flags, got %q", p.Error)
	}
}

func TestPropertiesSearchEmptyLegalOwnerAmongNamedRejected(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--legal-owner", "ACME LLC",
		"--legal-owner", "",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1 for an empty owner value, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(*queries) != 0 {
		t.Errorf("expected zero API requests, got %d", len(*queries))
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	// A named owner satisfies the scope requirement, so the empty value is
	// reported as the bad value it is.
	if !strings.Contains(p.Error, "empty") {
		t.Errorf("expected the empty-value message, got %q", p.Error)
	}
}

func TestPropertiesSearchTooManyLegalOwners(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	args := []string{"--base-url", srv.URL, "properties", "search"}
	for i := range 11 {
		args = append(args, "--legal-owner", fmt.Sprintf("OWNER %d", i))
	}

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env, args...)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1 for 11 owners, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(*queries) != 0 {
		t.Errorf("expected zero API requests, got %d", len(*queries))
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
}

func TestPropertiesSearchInvalidPermitFromShape(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "CA",
		"--permit-from", "2024-1-1",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(*queries) != 0 {
		t.Errorf("expected zero API requests, got %d", len(*queries))
	}
	p := parseStderrError(t, result.Stderr)
	if !strings.Contains(p.Error, "YYYY-MM-DD") {
		t.Errorf("expected the expected date shape in the message, got %q", p.Error)
	}
}

func TestPropertiesSearchInvalidCalendarDateForwarded(t *testing.T) {
	// 2024-13-01 satisfies the YYYY-MM-DD shape; calendar validity is the
	// API's 422 to raise, matching the decisions template.
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "CA",
		"--permit-from", "2024-13-01",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected the shape-valid date to be forwarded, got exit %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if got := (*queries)[0]["permit_from"]; len(got) != 1 || got[0] != "2024-13-01" {
		t.Errorf("expected permit_from=[2024-13-01] forwarded to the API, got %v", got)
	}
}

func TestPropertiesSearchRequiresAuth(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env, "properties", "search", "--geo-id", "CA")

	if result.ExitCode != 2 {
		t.Fatalf("expected exit 2 without an API key, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "auth_error" {
		t.Errorf("expected error_type auth_error, got %q", p.ErrorType)
	}
}

func TestPropertiesSearchAPIErrorExitCodeMapping(t *testing.T) {
	// API error responses must map to project exit codes via shared
	// client.APIError handling: auth=2, rate-limit=3, credit-exhausted=4,
	// server=5. The command layer must not remap them.
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
				"properties", "search",
				"--geo-id", "CA",
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

func TestPropertiesSearchExactlyTenLegalOwners(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	args := []string{"--base-url", srv.URL, "properties", "search"}
	for i := range 10 {
		args = append(args, "--legal-owner", fmt.Sprintf("OWNER %d", i))
	}

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env, args...)

	if result.ExitCode != 0 {
		t.Fatalf("expected 10 owners to be accepted, got exit %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if got := (*queries)[0]["legal_owner"]; len(got) != 10 {
		t.Errorf("expected 10 legal_owner params, got %d: %v", len(got), got)
	}
}
