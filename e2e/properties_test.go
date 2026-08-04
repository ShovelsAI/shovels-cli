//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
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

// propertyRangePairCases is the full inventory of property attribute range
// pairs: both bound flags, the parameters they map to, and values in the units
// each pair expects — one for the min == max case, and an inverted assignment.
// The names are spelled out here rather than read from the command's own
// tables, so a renamed or dropped bound fails a test instead of moving on both
// sides at once. TestPropertiesSearchAttributeRangesMapped keeps its own copy
// of the parameter names so the flag-to-parameter mapping has a witness that
// does not share this table.
var propertyRangePairCases = []struct {
	name                     string
	minFlag, minParam        string
	maxFlag, maxParam        string
	exact                    string
	invertedMin, invertedMax string
}{
	{
		name:    "MarketValue",
		minFlag: "--property-min-market-value", minParam: "property_min_market_value",
		maxFlag: "--property-max-market-value", maxParam: "property_max_market_value",
		exact: "50000000", invertedMin: "100000000", invertedMax: "50000000",
	},
	{
		name:    "LotSize",
		minFlag: "--property-min-lot-size", minParam: "property_min_lot_size",
		maxFlag: "--property-max-lot-size", maxParam: "property_max_lot_size",
		exact: "4000", invertedMin: "12000", invertedMax: "4000",
	},
	{
		name:    "BuildingArea",
		minFlag: "--property-min-building-area", minParam: "property_min_building_area",
		maxFlag: "--property-max-building-area", maxParam: "property_max_building_area",
		exact: "1200", invertedMin: "3500", invertedMax: "1200",
	},
	{
		name:    "UnitCount",
		minFlag: "--property-min-unit-count", minParam: "property_min_unit_count",
		maxFlag: "--property-max-unit-count", maxParam: "property_max_unit_count",
		exact: "4", invertedMin: "4", invertedMax: "1",
	},
	{
		name:    "YearBuilt",
		minFlag: "--property-min-year-built", minParam: "property_min_year_built",
		maxFlag: "--property-max-year-built", maxParam: "property_max_year_built",
		exact: "1985", invertedMin: "1990", invertedMax: "1950",
	},
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

func TestPropertiesSearchPropertyTypeRepeatsParam(t *testing.T) {
	// Comma-separating and repeating the flag are two spellings of one list,
	// and every spelling reaches the API as repeated property_type params in
	// the order given.
	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "CommaSeparated",
			args: []string{"--property-type", "residential,commercial"},
			want: []string{"residential", "commercial"},
		},
		{
			name: "RepeatedFlag",
			args: []string{"--property-type", "residential", "--property-type", "commercial"},
			want: []string{"residential", "commercial"},
		},
		{
			name: "BothForms",
			args: []string{"--property-type", "residential,commercial", "--property-type", "vacant land"},
			want: []string{"residential", "commercial", "vacant land"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
			srv := httptest.NewServer(handler)
			defer srv.Close()

			args := append([]string{
				"--base-url", srv.URL,
				"properties", "search",
				"--geo-id", "CA",
			}, tc.args...)

			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env, args...)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			got := (*queries)[0]["property_type"]
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("expected property_type=%v, got %v", tc.want, got)
			}
		})
	}
}

func TestPropertiesSearchAttributeRangesMapped(t *testing.T) {
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "CA",
		"--property-min-market-value", "50000000",
		"--property-max-market-value", "100000000",
		"--property-min-lot-size", "4000",
		"--property-max-lot-size", "12000",
		"--property-min-building-area", "1200",
		"--property-max-building-area", "3500",
		"--property-min-unit-count", "1",
		"--property-max-unit-count", "4",
		"--property-min-year-built", "1950",
		"--property-max-year-built", "1989",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	q := (*queries)[0]
	want := map[string]string{
		"property_min_market_value":  "50000000",
		"property_max_market_value":  "100000000",
		"property_min_lot_size":      "4000",
		"property_max_lot_size":      "12000",
		"property_min_building_area": "1200",
		"property_max_building_area": "3500",
		"property_min_unit_count":    "1",
		"property_max_unit_count":    "4",
		"property_min_year_built":    "1950",
		"property_max_year_built":    "1989",
	}
	for param, value := range want {
		if got := q[param]; len(got) != 1 || got[0] != value {
			t.Errorf("expected %s=[%q], got %v", param, value, got)
		}
	}
	// Permits search filters on story count; the Properties API does not.
	for _, param := range []string{"property_min_story_count", "property_max_story_count"} {
		if _, ok := q[param]; ok {
			t.Errorf("expected %s never to be sent, got %v", param, q[param])
		}
	}
}

func TestPropertiesSearchUnsetAttributeFiltersOmitted(t *testing.T) {
	// Every range bound defaults to 0, so a bound sent without its flag would
	// silently narrow the search instead of failing. The whole attribute
	// parameter inventory is swept, both when no attribute flag is given and
	// when one is, so a neighbouring bound riding along on a set flag is
	// caught too.
	inventory := []string{"property_type"}
	for _, pair := range propertyRangePairCases {
		inventory = append(inventory, pair.minParam, pair.maxParam)
	}

	cases := []struct {
		name string
		args []string
		// sent maps each parameter the given flags must emit to its value;
		// every other parameter in the inventory must be absent.
		sent map[string]string
	}{
		{
			name: "NoAttributeFlags",
		},
		{
			name: "OneAttributeFlagSet",
			args: []string{"--property-min-year-built", "1950"},
			sent: map[string]string{"property_min_year_built": "1950"},
		},
		{
			name: "OnlyTypeSet",
			args: []string{"--property-type", "residential"},
			sent: map[string]string{"property_type": "residential"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
			srv := httptest.NewServer(handler)
			defer srv.Close()

			args := append([]string{
				"--base-url", srv.URL,
				"properties", "search",
				"--geo-id", "CA",
			}, tc.args...)

			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env, args...)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}
			if len(*queries) != 1 {
				t.Fatalf("expected 1 API request, got %d", len(*queries))
			}

			q := (*queries)[0]
			for _, param := range inventory {
				want, isSet := tc.sent[param]
				if isSet {
					if got := q[param]; len(got) != 1 || got[0] != want {
						t.Errorf("expected %s=[%q], got %v", param, want, got)
					}
					continue
				}
				if got, ok := q[param]; ok {
					t.Errorf("expected %s absent when its flag is unset, got %v", param, got)
				}
			}
		})
	}
}

// --- Edge cases ---

func TestPropertiesSearchEqualRangeBoundsSent(t *testing.T) {
	// min == max is an exact-value match, not an empty range, and the rule
	// holds within any pair, so every one of the five is tried.
	for _, pair := range propertyRangePairCases {
		t.Run(pair.name, func(t *testing.T) {
			handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
			srv := httptest.NewServer(handler)
			defer srv.Close()

			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env,
				"--base-url", srv.URL,
				"properties", "search",
				"--geo-id", "CA",
				pair.minFlag, pair.exact,
				pair.maxFlag, pair.exact,
			)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0 for equal %s bounds, got %d; stderr: %s", pair.name, result.ExitCode, result.Stderr)
			}

			q := (*queries)[0]
			for _, param := range []string{pair.minParam, pair.maxParam} {
				if got := q[param]; len(got) != 1 || got[0] != pair.exact {
					t.Errorf("expected %s=[%q], got %v", param, pair.exact, got)
				}
			}
		})
	}
}

func TestPropertiesSearchUnknownPropertyTypeRejectedByServer(t *testing.T) {
	// The valid-type list is the API's to police, so an unknown value is
	// forwarded and the server's 422 is what the caller sees.
	var queries []map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := map[string][]string{}
		for k, v := range r.URL.Query() {
			params[k] = v
		}
		queries = append(queries, params)
		w.WriteHeader(422)
		w.Write([]byte(`{"detail":"property_type must be one of: residential, commercial, ..."}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "CA",
		"--property-type", "castle",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1 for a server-side 422, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(queries) != 1 {
		t.Fatalf("expected the unknown type to reach the API, got %d requests", len(queries))
	}
	if got := queries[0]["property_type"]; len(got) != 1 || got[0] != "castle" {
		t.Errorf("expected property_type=[castle] forwarded unchanged, got %v", got)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	if !strings.Contains(p.Error, "property_type") {
		t.Errorf("expected the API's own message to be surfaced, got %q", p.Error)
	}
}

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

func TestPropertiesSearchNegativeRangeBoundRejected(t *testing.T) {
	// The rule holds for any range bound, so every one of the ten is tried.
	for _, pair := range propertyRangePairCases {
		for _, flag := range []string{pair.minFlag, pair.maxFlag} {
			t.Run(flag, func(t *testing.T) {
				handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
				srv := httptest.NewServer(handler)
				defer srv.Close()

				env := withIsolatedConfig(t)
				result := runCLIWithEnv(t, env,
					"--base-url", srv.URL,
					"properties", "search",
					"--geo-id", "CA",
					flag, "-1",
				)

				if result.ExitCode != 1 {
					t.Fatalf("expected exit 1 for %s -1, got %d; stderr: %s", flag, result.ExitCode, result.Stderr)
				}
				if len(*queries) != 0 {
					t.Errorf("expected zero API requests, got %d", len(*queries))
				}
				p := parseStderrError(t, result.Stderr)
				if p.ErrorType != "validation_error" {
					t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
				}
				if !strings.Contains(p.Error, flag) {
					t.Errorf("expected the message to name %s, got %q", flag, p.Error)
				}
			})
		}
	}
}

func TestPropertiesSearchInvertedRangePairRejected(t *testing.T) {
	// The rule holds within any pair, so every one of the five is tried.
	for _, pair := range propertyRangePairCases {
		t.Run(pair.name, func(t *testing.T) {
			handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
			srv := httptest.NewServer(handler)
			defer srv.Close()

			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env,
				"--base-url", srv.URL,
				"properties", "search",
				"--geo-id", "CA",
				pair.minFlag, pair.invertedMin,
				pair.maxFlag, pair.invertedMax,
			)

			if result.ExitCode != 1 {
				t.Fatalf("expected exit 1 for an inverted %s pair, got %d; stderr: %s", pair.name, result.ExitCode, result.Stderr)
			}
			if len(*queries) != 0 {
				t.Errorf("expected zero API requests, got %d", len(*queries))
			}
			p := parseStderrError(t, result.Stderr)
			if p.ErrorType != "validation_error" {
				t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
			}
			if !strings.Contains(p.Error, pair.minFlag) || !strings.Contains(p.Error, pair.maxFlag) {
				t.Errorf("expected the message to name both bounds of the pair, got %q", p.Error)
			}
		})
	}
}

func TestPropertiesSearchRangeErrorPrecedence(t *testing.T) {
	// Negativity is checked across every bound before any pair is checked for
	// inversion, so a pair violating both rules reports the negative value.
	// Both branches are pinned: the message that wins, and the message that
	// surfaces when only inversion applies.
	cases := []struct {
		name       string
		lower      string
		upper      string
		wantSubstr string
		denySubstr string
	}{
		{
			name:       "NegativeAndInverted",
			lower:      "-5",
			upper:      "-10",
			wantSubstr: "must not be negative",
			denySubstr: "must not exceed",
		},
		{
			name:       "InvertedOnly",
			lower:      "10",
			upper:      "5",
			wantSubstr: "must not exceed",
			denySubstr: "must not be negative",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
			srv := httptest.NewServer(handler)
			defer srv.Close()

			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env,
				"--base-url", srv.URL,
				"properties", "search",
				"--geo-id", "CA",
				"--property-min-market-value", tc.lower,
				"--property-max-market-value", tc.upper,
			)

			if result.ExitCode != 1 {
				t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}
			if len(*queries) != 0 {
				t.Errorf("expected zero API requests, got %d", len(*queries))
			}
			p := parseStderrError(t, result.Stderr)
			if !strings.Contains(p.Error, tc.wantSubstr) {
				t.Errorf("expected the message to contain %q, got %q", tc.wantSubstr, p.Error)
			}
			if strings.Contains(p.Error, tc.denySubstr) {
				t.Errorf("expected the message not to contain %q, got %q", tc.denySubstr, p.Error)
			}
		})
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

func TestPropertiesSearchExplicitZeroRangeBoundSent(t *testing.T) {
	// 0 is also the flag's default, so only explicit-set detection tells
	// "at least zero units" apart from "no unit-count filter".
	handler, queries := makePropertiesSearchHandler(singlePropertiesPage(1))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "search",
		"--geo-id", "CA",
		"--property-min-unit-count", "0",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	q := (*queries)[0]
	if got := q["property_min_unit_count"]; len(got) != 1 || got[0] != "0" {
		t.Errorf("expected property_min_unit_count=[0], got %v", got)
	}
	if _, ok := q["property_max_unit_count"]; ok {
		t.Errorf("expected the unset counterpart to stay absent, got %v", q["property_max_unit_count"])
	}
}

// =======================================================================
// properties get
// =======================================================================

// makePropertiesGetHandler serves batch /properties responses and records each
// request's id parameters, so a test can assert both the order they were sent
// in and that no request was made at all. Any request that is not
// GET /properties is refused, so a wrong verb or path fails the test.
// knownIDs decides which IDs exist: the endpoint answers an address id it
// cannot resolve by omitting its row rather than failing, and answers a
// repeated id with a single row, so the handler does both.
func makePropertiesGetHandler(knownIDs map[string]bool, creditsUsed, creditsRemaining int) (http.Handler, *[][]string) {
	requests := &[][]string{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/properties" {
			w.WriteHeader(404)
			w.Write([]byte(`{"detail":"unexpected method or path"}`))
			return
		}

		ids := r.URL.Query()["id"]
		*requests = append(*requests, ids)

		// Each row is shaped like the endpoint's own, trimmed to the fields
		// these tests read.
		items := []json.RawMessage{}
		emitted := map[string]bool{}
		for _, id := range ids {
			if knownIDs[id] && !emitted[id] {
				emitted[id] = true
				items = append(items, json.RawMessage(fmt.Sprintf(
					`{"id":%q,"street_no":"3658","street":"LORIMER LN","city":"ENCINITAS","state":"CA","property_type":"residential","year_built":1986}`,
					id,
				)))
			}
		}

		w.Header().Set("X-Credits-Request", strconv.Itoa(creditsUsed))
		w.Header().Set("X-Credits-Remaining", strconv.Itoa(creditsRemaining))

		// The endpoint carries paginator fields on this batch response; the
		// CLI must ignore them rather than surface them as batch meta.
		resp := struct {
			Items      []json.RawMessage `json:"items"`
			Size       int               `json:"size"`
			NextCursor *string           `json:"next_cursor"`
			TotalCount *int              `json:"total_count"`
		}{Items: items, Size: len(items)}
		json.NewEncoder(w).Encode(resp)
	})

	return handler, requests
}

// propertyGetIDs builds n distinct address IDs.
func propertyGetIDs(n int) []string {
	ids := make([]string, n)
	for i := range n {
		ids[i] = fmt.Sprintf("a_%05d", i)
	}
	return ids
}

// knownPropertyIDs turns an ID list into the handler's existence map.
func knownPropertyIDs(ids []string) map[string]bool {
	known := make(map[string]bool, len(ids))
	for _, id := range ids {
		known[id] = true
	}
	return known
}

// assertNoHasMore fails if a batch envelope carries has_more, which belongs to
// paginated responses only.
func assertNoHasMore(t *testing.T, meta map[string]any) {
	t.Helper()
	if v, ok := meta["has_more"]; ok {
		t.Errorf("batch meta must never carry has_more, got %v", v)
	}
}

// assertNoMissing fails if meta.missing is present at all.
func assertNoMissing(t *testing.T, meta map[string]any) {
	t.Helper()
	if v, ok := meta["missing"]; ok {
		t.Errorf("expected meta.missing to be omitted when every ID resolved, got %v", v)
	}
}

// metaMissing returns meta.missing as strings, failing if the key is absent.
func metaMissing(t *testing.T, meta map[string]any) []string {
	t.Helper()
	raw, ok := meta["missing"]
	if !ok {
		t.Fatalf("expected meta.missing, got meta: %v", meta)
	}
	entries, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected meta.missing to be an array, got %T", raw)
	}
	ids := make([]string, len(entries))
	for i, entry := range entries {
		s, ok := entry.(string)
		if !ok {
			t.Fatalf("expected meta.missing[%d] to be a string, got %T", i, entry)
		}
		ids[i] = s
	}
	return ids
}

// --- properties get: Happy paths ---

func TestPropertiesGetSingleID(t *testing.T) {
	handler, requests := makePropertiesGetHandler(knownPropertyIDs([]string{"a_123"}), 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "get", "a_123",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// The handler refuses anything but GET /properties, so reaching it at all
	// pins the verb and path.
	if len(*requests) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*requests))
	}
	if got := (*requests)[0]; !reflect.DeepEqual(got, []string{"a_123"}) {
		t.Errorf("expected id=[a_123] forwarded, got %v", got)
	}

	parsed := parseEnvelope(t, result.Stdout)
	if got := len(dataRows(t, parsed)); got != 1 {
		t.Errorf("expected 1 row, got %d", got)
	}
	if int(parsed.Meta["count"].(float64)) != 1 {
		t.Errorf("expected count=1, got %v", parsed.Meta["count"])
	}
	if int(parsed.Meta["credits_used"].(float64)) != 1 {
		t.Errorf("expected credits_used=1, got %v", parsed.Meta["credits_used"])
	}
	if int(parsed.Meta["credits_remaining"].(float64)) != 9999 {
		t.Errorf("expected credits_remaining=9999, got %v", parsed.Meta["credits_remaining"])
	}
	assertNoMissing(t, parsed.Meta)
	assertNoHasMore(t, parsed.Meta)
}

func TestPropertiesGetMultipleIDsInArgumentOrder(t *testing.T) {
	// The IDs are deliberately out of sorted order, so a request that sorted
	// or reordered them would fail the assertion.
	args := []string{"a_zzz", "a_aaa", "a_mmm"}
	handler, requests := makePropertiesGetHandler(knownPropertyIDs(args), 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		append([]string{"--base-url", srv.URL, "properties", "get"}, args...)...,
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if len(*requests) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*requests))
	}
	if got := (*requests)[0]; !reflect.DeepEqual(got, args) {
		t.Errorf("expected repeated id params in argument order %v, got %v", args, got)
	}

	parsed := parseEnvelope(t, result.Stdout)
	if got := len(dataRows(t, parsed)); got != 3 {
		t.Errorf("expected 3 rows, got %d", got)
	}
	if int(parsed.Meta["count"].(float64)) != 3 {
		t.Errorf("expected count=3, got %v", parsed.Meta["count"])
	}
	assertNoMissing(t, parsed.Meta)
	assertNoHasMore(t, parsed.Meta)
}

func TestPropertiesGetDryRun(t *testing.T) {
	// The server exists only to prove nothing reaches it, and no API key is
	// configured, so a real call could not succeed either.
	handler, requests := makePropertiesGetHandler(knownPropertyIDs([]string{"a_123", "a_456"}), 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "get", "a_123", "a_456",
		"--dry-run",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(*requests) != 0 {
		t.Errorf("expected zero API requests under --dry-run, got %d", len(*requests))
	}

	out := parseDryRun(t, result.Stdout)
	if out.Method != "GET" {
		t.Errorf("expected method GET, got %q", out.Method)
	}
	if !strings.HasSuffix(out.URL, "/properties") {
		t.Errorf("expected URL ending with /properties, got %q", out.URL)
	}
	ids, ok := out.Params["id"].([]any)
	if !ok || len(ids) != 2 || ids[0] != "a_123" || ids[1] != "a_456" {
		t.Errorf("expected id=[a_123 a_456], got %v", out.Params["id"])
	}
}

// --- properties get: Edge cases ---

func TestPropertiesGetUnknownIDAmongKnown(t *testing.T) {
	handler, requests := makePropertiesGetHandler(knownPropertyIDs([]string{"a_123", "a_789"}), 2, 9998)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "get", "a_123", "a_gone", "a_789",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if got := (*requests)[0]; !reflect.DeepEqual(got, []string{"a_123", "a_gone", "a_789"}) {
		t.Errorf("expected the unresolved id to be sent alongside the others, got %v", got)
	}

	parsed := parseEnvelope(t, result.Stdout)
	if got := len(dataRows(t, parsed)); got != 2 {
		t.Errorf("expected 2 rows, got %d", got)
	}
	if int(parsed.Meta["count"].(float64)) != 2 {
		t.Errorf("expected count=2, got %v", parsed.Meta["count"])
	}
	if got := metaMissing(t, parsed.Meta); !reflect.DeepEqual(got, []string{"a_gone"}) {
		t.Errorf("expected missing=[a_gone], got %v", got)
	}
	assertNoHasMore(t, parsed.Meta)
}

func TestPropertiesGetAllIDsUnknown(t *testing.T) {
	handler, _ := makePropertiesGetHandler(map[string]bool{}, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "get", "a_gone", "a_lost", "a_void",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 when no ID resolves, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	if got := len(dataRows(t, parsed)); got != 0 {
		t.Errorf("expected an empty data array, got %d rows", got)
	}
	if got := strings.TrimSpace(string(parsed.Data)); got != "[]" {
		t.Errorf("expected data to be [] rather than null, got %s", got)
	}
	if int(parsed.Meta["count"].(float64)) != 0 {
		t.Errorf("expected count=0, got %v", parsed.Meta["count"])
	}
	want := []string{"a_gone", "a_lost", "a_void"}
	if got := metaMissing(t, parsed.Meta); !reflect.DeepEqual(got, want) {
		t.Errorf("expected every requested id in missing, in request order %v, got %v", want, got)
	}
	assertNoHasMore(t, parsed.Meta)
}

func TestPropertiesGetDuplicateKnownIDForwardedUnchanged(t *testing.T) {
	handler, requests := makePropertiesGetHandler(knownPropertyIDs([]string{"a_123"}), 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "get", "a_123", "a_123",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Duplicates are not collapsed before the call: the CLI sends what it was
	// given and lets the endpoint decide.
	if got := (*requests)[0]; !reflect.DeepEqual(got, []string{"a_123", "a_123"}) {
		t.Errorf("expected both duplicate ids forwarded unchanged, got %v", got)
	}

	parsed := parseEnvelope(t, result.Stdout)
	if got := len(dataRows(t, parsed)); got != 1 {
		t.Errorf("expected the single row the endpoint returns for a repeated id, got %d", got)
	}
	assertNoMissing(t, parsed.Meta)
	assertNoHasMore(t, parsed.Meta)
}

func TestPropertiesGetDuplicateUnknownIDCountedPerOccurrence(t *testing.T) {
	handler, requests := makePropertiesGetHandler(knownPropertyIDs([]string{"a_123"}), 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "get", "a_gone", "a_123", "a_gone",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if got := (*requests)[0]; !reflect.DeepEqual(got, []string{"a_gone", "a_123", "a_gone"}) {
		t.Errorf("expected all three ids forwarded unchanged, got %v", got)
	}

	parsed := parseEnvelope(t, result.Stdout)
	want := []string{"a_gone", "a_gone"}
	if got := metaMissing(t, parsed.Meta); !reflect.DeepEqual(got, want) {
		t.Errorf("expected one missing entry per unmatched occurrence %v, got %v", want, got)
	}
	assertNoHasMore(t, parsed.Meta)
}

func TestPropertiesGetGeolocationIDRejectedByServer(t *testing.T) {
	// City, county, and jurisdiction ids are opaque, so only the server can
	// reject them; the CLI must forward the request and surface the API's 422.
	var requests [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Query()["id"])
		w.WriteHeader(422)
		w.Write([]byte(`{"detail":{"loc":["query","id",0],"msg":"Property id 'RMjg6rIIh2k' at position 0 is a city, county, or jurisdiction id, not an ADDRESS id. Pass ADDRESS ids from /addresses/search.","type":"value_error"}}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "get", "RMjg6rIIh2k",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1 for a server-side 422, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(requests) != 1 {
		t.Fatalf("expected the opaque id to reach the API, got %d requests", len(requests))
	}
	if got := requests[0]; !reflect.DeepEqual(got, []string{"RMjg6rIIh2k"}) {
		t.Errorf("expected id=[RMjg6rIIh2k] forwarded unchanged, got %v", got)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	if !strings.Contains(p.Error, "ADDRESS id") {
		t.Errorf("expected the API's own message to be surfaced, got %q", p.Error)
	}
}

// --- properties get: Error conditions ---

func TestPropertiesGetNoIDs(t *testing.T) {
	handler, requests := makePropertiesGetHandler(map[string]bool{}, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"properties", "get",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(*requests) != 0 {
		t.Errorf("expected zero API requests, got %d", len(*requests))
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	if p.Error != "at least one property ID required" {
		t.Errorf("expected error %q, got %q", "at least one property ID required", p.Error)
	}
}

func TestPropertiesGetTooManyIDs(t *testing.T) {
	handler, requests := makePropertiesGetHandler(map[string]bool{}, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	args := append([]string{"--base-url", srv.URL, "properties", "get"}, propertyGetIDs(51)...)
	result := runCLIWithEnv(t, env, args...)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(*requests) != 0 {
		t.Errorf("expected zero API requests when the ID count is rejected, got %d", len(*requests))
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type validation_error, got %q", p.ErrorType)
	}
	if p.Error != "maximum 50 IDs per request" {
		t.Errorf("expected error %q, got %q", "maximum 50 IDs per request", p.Error)
	}
}

func TestPropertiesGetRequiresAuth(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env, "properties", "get", "a_123")

	if result.ExitCode != 2 {
		t.Fatalf("expected exit 2 without an API key, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "auth_error" {
		t.Errorf("expected error_type auth_error, got %q", p.ErrorType)
	}
}

func TestPropertiesGetAPIErrorExitCodeMapping(t *testing.T) {
	// API error responses must map to project exit codes via shared
	// client.APIError handling: auth=2, rate-limit=3, credit-exhausted=4,
	// server=5, any other 4xx=1. The command layer must not remap them.
	cases := []struct {
		status   int
		exitCode int
		errType  string
	}{
		{401, 2, "auth_error"},
		{402, 4, "credit_exhausted"},
		{404, 1, "validation_error"},
		{422, 1, "validation_error"},
		{429, 3, "rate_limited"},
		{500, 5, "server_error"},
		{503, 5, "server_error"},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d", tc.status), func(t *testing.T) {
			srv := httptest.NewServer(makeStatusHandler(tc.status))
			defer srv.Close()

			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env,
				"--base-url", srv.URL,
				"--no-retry",
				"properties", "get", "a_123",
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

func TestPropertiesGetTransportFailureExitsCode5(t *testing.T) {
	// Port 1 refuses connections, so the request never reaches a server.
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", "http://127.0.0.1:1",
		"--no-retry",
		"--timeout", "2s",
		"properties", "get", "a_123",
	)

	if result.ExitCode != 5 {
		t.Fatalf("expected exit 5 for a transport failure, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "network_error" {
		t.Errorf("expected error_type network_error, got %q", p.ErrorType)
	}
}

func TestPropertiesGetTimeoutExitsCode5(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"--no-retry",
		"--timeout", "1s",
		"properties", "get", "a_123",
	)

	if result.ExitCode != 5 {
		t.Fatalf("expected exit 5 when the request times out, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "network_error" {
		t.Errorf("expected error_type network_error, got %q", p.ErrorType)
	}
}

// --- properties get: Boundary conditions ---

func TestPropertiesGetExactly50IDs(t *testing.T) {
	ids := propertyGetIDs(50)
	handler, requests := makePropertiesGetHandler(knownPropertyIDs(ids), 50, 9950)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	args := append([]string{"--base-url", srv.URL, "properties", "get"}, ids...)
	result := runCLIWithEnv(t, env, args...)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 for exactly 50 IDs, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if got := (*requests)[0]; !reflect.DeepEqual(got, ids) {
		t.Errorf("expected all 50 ids forwarded in argument order, got %v", got)
	}

	parsed := parseEnvelope(t, result.Stdout)
	if got := len(dataRows(t, parsed)); got != 50 {
		t.Errorf("expected 50 rows, got %d", got)
	}
	if int(parsed.Meta["count"].(float64)) != 50 {
		t.Errorf("expected count=50, got %v", parsed.Meta["count"])
	}
	assertNoMissing(t, parsed.Meta)
	assertNoHasMore(t, parsed.Meta)
}
