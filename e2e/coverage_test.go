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

// coverageItem mirrors a single item returned by the coverage endpoint.
type coverageItem struct {
	Field        string  `json:"field"`
	Tier         string  `json:"tier"`
	FillPct      float64 `json:"fill_pct"`
	PermitsTotal int     `json:"permits_total"`
}

// makeCoverageHandler returns an HTTP handler that serves a coverage response
// with the supplied items and captures every query it receives. It validates
// the required query params and 422s if any are missing.
func makeCoverageHandler(items []coverageItem) (http.Handler, *[]map[string][]string, *[]string) {
	captured := &[]map[string][]string{}
	paths := &[]string{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.Path)
		params := map[string][]string{}
		for k, v := range r.URL.Query() {
			params[k] = v
		}
		*captured = append(*captured, params)

		for _, required := range []string{"geo_type", "geo_id", "date_from", "date_to"} {
			if r.URL.Query().Get(required) == "" {
				w.WriteHeader(422)
				w.Write([]byte(`{"detail":"` + required + ` is required"}`))
				return
			}
		}

		resp := struct {
			Items []coverageItem `json:"items"`
		}{Items: items}
		json.NewEncoder(w).Encode(resp)
	})

	return handler, captured, paths
}

// makeStatusHandler returns a handler that always responds with the given
// status code and a JSON detail body.
func makeStatusHandler(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(`{"detail":"error"}`))
	})
}

// makeRawBodyHandler returns a handler that responds 200 with a raw body.
func makeRawBodyHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(body))
	})
}

// --- Happy paths ---

func TestCoverageBasic(t *testing.T) {
	handler, queries, paths := makeCoverageHandler([]coverageItem{
		{Field: "fees", Tier: "missing", FillPct: 0.02, PermitsTotal: 12034},
		{Field: "job_value", Tier: "partial", FillPct: 0.41, PermitsTotal: 12034},
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"zipcodes", "coverage", "92024",
		"--coverage-from", "2024-01-01",
		"--coverage-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []coverageItem
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array of coverage items: %v\ndata: %s", err, parsed.Data)
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 items, got %d", len(data))
	}
	if data[0].Field != "fees" || data[0].Tier != "missing" {
		t.Errorf("unexpected first item: %+v", data[0])
	}
	if data[1].Field != "job_value" || data[1].Tier != "partial" {
		t.Errorf("unexpected second item: %+v", data[1])
	}

	// meta must be empty (credit-exempt, non-paginated).
	if len(parsed.Meta) != 0 {
		t.Errorf("expected empty meta, got %v", parsed.Meta)
	}

	// Verify endpoint path and query params.
	if len(*paths) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*paths))
	}
	if (*paths)[0] != "/meta/coverage" {
		t.Errorf("expected path /meta/coverage, got %q", (*paths)[0])
	}
	q := (*queries)[0]
	if q["geo_type"][0] != "zipcode" {
		t.Errorf("expected geo_type=zipcode, got %q", q["geo_type"][0])
	}
	if q["geo_id"][0] != "92024" {
		t.Errorf("expected geo_id=92024, got %q", q["geo_id"][0])
	}
	if q["date_from"][0] != "2024-01-01" {
		t.Errorf("expected date_from=2024-01-01, got %q", q["date_from"][0])
	}
	if q["date_to"][0] != "2024-12-31" {
		t.Errorf("expected date_to=2024-12-31, got %q", q["date_to"][0])
	}
	// Coverage must not send a size param (not paginated).
	if _, ok := q["size"]; ok {
		t.Errorf("coverage must not send size param, got %v", q["size"])
	}
}

func TestCoverageGeoTypePerCommand(t *testing.T) {
	cases := []struct {
		args    []string
		geoID   string
		geoType string
	}{
		{[]string{"cities", "coverage", "CITYID"}, "CITYID", "city"},
		{[]string{"counties", "coverage", "COUNTYID"}, "COUNTYID", "county"},
		{[]string{"jurisdictions", "coverage", "JURID"}, "JURID", "jurisdiction"},
		{[]string{"states", "coverage", "CA"}, "CA", "state"},
		{[]string{"zipcodes", "coverage", "92024"}, "92024", "zipcode"},
	}

	for _, tc := range cases {
		t.Run(tc.geoType, func(t *testing.T) {
			handler, queries, _ := makeCoverageHandler([]coverageItem{})
			srv := httptest.NewServer(handler)
			defer srv.Close()

			env := withIsolatedConfig(t)
			args := append([]string{"--base-url", srv.URL}, tc.args...)
			args = append(args, "--coverage-from", "2024-01-01", "--coverage-to", "2024-12-31")
			result := runCLIWithEnv(t, env, args...)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}
			if len(*queries) != 1 {
				t.Fatalf("expected 1 request, got %d", len(*queries))
			}
			q := (*queries)[0]
			if q["geo_type"][0] != tc.geoType {
				t.Errorf("expected geo_type=%q, got %q", tc.geoType, q["geo_type"][0])
			}
			if q["geo_id"][0] != tc.geoID {
				t.Errorf("expected geo_id=%q, got %q", tc.geoID, q["geo_id"][0])
			}
		})
	}
}

func TestCoverageReturnedSubsetUnchanged(t *testing.T) {
	// API returns a subset (reliable fields omitted); order/content preserved.
	items := []coverageItem{
		{Field: "job_value", Tier: "partial", FillPct: 0.55, PermitsTotal: 900},
		{Field: "fees", Tier: "missing", FillPct: 0.01, PermitsTotal: 900},
	}
	handler, _, _ := makeCoverageHandler(items)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "coverage", "ABC",
		"--coverage-from", "2024-01-01",
		"--coverage-to", "2024-12-31",
	)
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	var data []coverageItem
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 2 {
		t.Fatalf("expected 2 items, got %d", len(data))
	}
	if data[0].Field != "job_value" || data[1].Field != "fees" {
		t.Errorf("expected order preserved, got %q then %q", data[0].Field, data[1].Field)
	}
}

// --- Edge cases ---

func TestCoverageEmptyWindowAllMissing(t *testing.T) {
	items := make([]coverageItem, 10)
	for i := range items {
		items[i] = coverageItem{Field: "f" + string(rune('a'+i)), Tier: "missing", FillPct: 0, PermitsTotal: 0}
	}
	handler, _, _ := makeCoverageHandler(items)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"zipcodes", "coverage", "00000",
		"--coverage-from", "1900-01-01",
		"--coverage-to", "1900-01-02",
	)
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	parsed := parseEnvelope(t, result.Stdout)
	var data []coverageItem
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 10 {
		t.Errorf("expected 10 items, got %d", len(data))
	}
}

func TestCoverageEmptyItemsArray(t *testing.T) {
	handler, _, _ := makeCoverageHandler([]coverageItem{})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"states", "coverage", "CA",
		"--coverage-from", "2024-01-01",
		"--coverage-to", "2024-12-31",
	)
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	parsed := parseEnvelope(t, result.Stdout)
	var data []coverageItem
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array (possibly empty): %v\ndata: %s", err, parsed.Data)
	}
	if len(data) != 0 {
		t.Errorf("expected 0 items, got %d", len(data))
	}
}

func TestCoverageDryRun(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"states", "coverage", "CA",
		"--coverage-from", "2024-01-01",
		"--coverage-to", "2024-12-31",
		"--dry-run",
	)
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := parseDryRun(t, result.Stdout)
	if out.Method != "GET" {
		t.Errorf("expected method GET, got %q", out.Method)
	}
	if !strings.HasSuffix(out.URL, "/meta/coverage") {
		t.Errorf("expected URL ending with /meta/coverage, got %q", out.URL)
	}
	if out.Params["geo_type"] != "state" {
		t.Errorf("expected geo_type=state, got %v", out.Params["geo_type"])
	}
	if out.Params["geo_id"] != "CA" {
		t.Errorf("expected geo_id=CA, got %v", out.Params["geo_id"])
	}
	if out.Params["date_from"] != "2024-01-01" {
		t.Errorf("expected date_from=2024-01-01, got %v", out.Params["date_from"])
	}
	if out.Params["date_to"] != "2024-12-31" {
		t.Errorf("expected date_to=2024-12-31, got %v", out.Params["date_to"])
	}
	if _, ok := out.Params["size"]; ok {
		t.Errorf("dry-run must not include size param, got %v", out.Params["size"])
	}
}

func TestCoverageGeoIDForwardedVerbatim(t *testing.T) {
	cases := []string{"ca", "92024-1234"}
	for _, geoID := range cases {
		t.Run(geoID, func(t *testing.T) {
			handler, queries, _ := makeCoverageHandler([]coverageItem{})
			srv := httptest.NewServer(handler)
			defer srv.Close()

			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env,
				"--base-url", srv.URL,
				"zipcodes", "coverage", geoID,
				"--coverage-from", "2024-01-01",
				"--coverage-to", "2024-12-31",
			)
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}
			q := (*queries)[0]
			if q["geo_id"][0] != geoID {
				t.Errorf("expected geo_id forwarded verbatim as %q, got %q", geoID, q["geo_id"][0])
			}
		})
	}
}

// Coverage reports every field's tier for the window, so there is no record
// count to bound. The assertion on queries is what pins rejection ahead of the
// request: a rejection reached afterwards has already spent the round trip on a
// result the caller is not getting.
func TestCoverageRejectsGlobalLimitBeforeRequesting(t *testing.T) {
	handler, queries, _ := makeCoverageHandler([]coverageItem{{Field: "fees", Tier: "missing"}})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"zipcodes", "coverage", "92024",
		"--coverage-from", "2024-01-01",
		"--coverage-to", "2024-12-31",
		"--limit", "5",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(*queries) != 0 {
		t.Errorf("expected no request, got %d", len(*queries))
	}
}

// --max-records is the other bound coverage has nothing to apply. It is a
// separate entry in the API-only set, so a set that named only --limit would
// leave this one silently accepted and ignored.
func TestCoverageRejectsGlobalMaxRecordsBeforeRequesting(t *testing.T) {
	handler, queries, _ := makeCoverageHandler([]coverageItem{{Field: "fees", Tier: "missing"}})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"zipcodes", "coverage", "92024",
		"--coverage-from", "2024-01-01",
		"--coverage-to", "2024-12-31",
		"--max-records", "5",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(*queries) != 0 {
		t.Errorf("expected no request, got %d", len(*queries))
	}
}

// --- Error conditions ---

func TestCoverageMissingArg(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"zipcodes", "coverage",
		"--coverage-from", "2024-01-01",
		"--coverage-to", "2024-12-31",
	)
	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected code 1, got %d", p.Code)
	}
	if !strings.Contains(p.Error, "accepts 1 arg(s)") {
		t.Errorf("expected cobra argument error, got: %s", p.Error)
	}
}

func TestCoverageMissingDateFlags(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"zipcodes", "coverage", "92024",
	)
	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if hits.Load() != 0 {
		t.Errorf("expected no network call when date flags are missing, got %d", hits.Load())
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected validation_error, got %q", p.ErrorType)
	}
	if !strings.Contains(p.Error, "--coverage-from") {
		t.Errorf("expected error to mention --coverage-from, got: %s", p.Error)
	}
	if !strings.Contains(p.Error, "--coverage-to") {
		t.Errorf("expected error to mention --coverage-to, got: %s", p.Error)
	}
}

func TestCoverageMissingSingleDateFlag(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"zipcodes", "coverage", "92024",
		"--coverage-from", "2024-01-01",
	)
	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if hits.Load() != 0 {
		t.Errorf("expected no network call when a date flag is missing, got %d", hits.Load())
	}
	p := parseStderrError(t, result.Stderr)
	if !strings.Contains(p.Error, "--coverage-to") {
		t.Errorf("expected error to mention --coverage-to, got: %s", p.Error)
	}
	if strings.Contains(p.Error, "--coverage-from") {
		t.Errorf("did not expect --coverage-from in error, got: %s", p.Error)
	}
}

func TestCoverageBadDateFormat(t *testing.T) {
	cases := []struct {
		flag string
		val  string
		args []string
	}{
		{"--coverage-from", "2024/01/01", []string{"--coverage-from", "2024/01/01", "--coverage-to", "2024-12-31"}},
		{"--coverage-from", "2024-1-1", []string{"--coverage-from", "2024-1-1", "--coverage-to", "2024-12-31"}},
		{"--coverage-to", "last week", []string{"--coverage-from", "2024-01-01", "--coverage-to", "last week"}},
	}
	for _, tc := range cases {
		t.Run(tc.val, func(t *testing.T) {
			env := withIsolatedConfig(t)
			args := append([]string{"zipcodes", "coverage", "92024"}, tc.args...)
			result := runCLIWithEnv(t, env, args...)
			if result.ExitCode != 1 {
				t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}
			p := parseStderrError(t, result.Stderr)
			if p.ErrorType != "validation_error" {
				t.Errorf("expected validation_error, got %q", p.ErrorType)
			}
			if !strings.Contains(p.Error, "invalid date format for "+tc.flag) {
				t.Errorf("expected error about %s, got: %s", tc.flag, p.Error)
			}
			if !strings.Contains(p.Error, "YYYY-MM-DD") {
				t.Errorf("expected YYYY-MM-DD hint, got: %s", p.Error)
			}
		})
	}
}

func TestCoverageBadDateMakesNoNetworkCall(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"zipcodes", "coverage", "92024",
		"--coverage-from", "2024/01/01",
		"--coverage-to", "2024-12-31",
	)
	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if hits.Load() != 0 {
		t.Errorf("expected no network call on bad date, got %d", hits.Load())
	}
}

func TestCoverageRequiresAuth(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env,
		"zipcodes", "coverage", "92024",
		"--coverage-from", "2024-01-01",
		"--coverage-to", "2024-12-31",
	)
	if result.ExitCode != 2 {
		t.Fatalf("expected exit 2, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "auth_error" {
		t.Errorf("expected auth_error, got %q", p.ErrorType)
	}
}

func TestCoverageStatusCodeMapping(t *testing.T) {
	cases := []struct {
		status   int
		exitCode int
		errType  string
	}{
		{401, 2, "auth_error"},
		{402, 4, "credit_exhausted"},
		{422, 1, "validation_error"},
		{429, 3, "rate_limited"},
		{500, 5, "server_error"},
		{503, 5, "server_error"},
		{403, 1, "validation_error"},
	}
	for _, tc := range cases {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(makeStatusHandler(tc.status))
			defer srv.Close()

			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env,
				"--base-url", srv.URL,
				"--no-retry",
				"zipcodes", "coverage", "92024",
				"--coverage-from", "2024-01-01",
				"--coverage-to", "2024-12-31",
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

func TestCoverageMalformedBody(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"not-json", `not json`},
		{"missing-items", `{"foo":"bar"}`},
		{"items-null", `{"items":null}`},
		{"items-not-array-object", `{"items":{}}`},
		{"items-not-array-string", `{"items":"nope"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(makeRawBodyHandler(tc.body))
			defer srv.Close()

			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env,
				"--base-url", srv.URL,
				"zipcodes", "coverage", "92024",
				"--coverage-from", "2024-01-01",
				"--coverage-to", "2024-12-31",
			)
			if result.ExitCode != 1 {
				t.Fatalf("%s: expected exit 1, got %d; stdout: %s stderr: %s", tc.name, result.ExitCode, result.Stdout, result.Stderr)
			}
			// stdout must NOT carry a partial/empty envelope.
			if strings.TrimSpace(result.Stdout) != "" {
				t.Errorf("%s: expected empty stdout, got: %s", tc.name, result.Stdout)
			}
			p := parseStderrError(t, result.Stderr)
			if p.ErrorType != "client_error" {
				t.Errorf("%s: expected client_error, got %q", tc.name, p.ErrorType)
			}
		})
	}
}

// --- Boundary conditions ---

func TestCoverageSingleDayWindow(t *testing.T) {
	handler, queries, _ := makeCoverageHandler([]coverageItem{})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"zipcodes", "coverage", "92024",
		"--coverage-from", "2024-06-01",
		"--coverage-to", "2024-06-01",
	)
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	q := (*queries)[0]
	if q["date_from"][0] != "2024-06-01" || q["date_to"][0] != "2024-06-01" {
		t.Errorf("expected both dates forwarded as 2024-06-01, got from=%q to=%q", q["date_from"][0], q["date_to"][0])
	}
}

func TestCoverageInvertedRangeDelegatedToAPI(t *testing.T) {
	// from > to: no client-side rejection; the request proceeds and the API
	// decides (here a 422). The key assertion is that the CLI made the call.
	handler, queries, _ := makeCoverageHandler([]coverageItem{})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"zipcodes", "coverage", "92024",
		"--coverage-from", "2024-12-31",
		"--coverage-to", "2024-01-01",
	)
	// Handler accepts any well-formed dates → exit 0; the CLI did not reject.
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 (ordering delegated to API), got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if len(*queries) != 1 {
		t.Fatalf("expected the CLI to make the request, got %d", len(*queries))
	}
	q := (*queries)[0]
	if q["date_from"][0] != "2024-12-31" || q["date_to"][0] != "2024-01-01" {
		t.Errorf("expected inverted range forwarded verbatim, got from=%q to=%q", q["date_from"][0], q["date_to"][0])
	}
}

func TestCoverageSchemaFlagSkipsArgAndNetwork(t *testing.T) {
	// With --schema, the missing GEO_ID is allowed (no cobra arg error) and no
	// network call is made; this test asserts only that arg/network behavior.
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"zipcodes", "coverage", "--schema",
	)
	if hits.Load() != 0 {
		t.Errorf("expected no network call with --schema, got %d", hits.Load())
	}
	if strings.Contains(result.Stderr, "accepts 1 arg(s)") {
		t.Errorf("--schema must allow missing GEO_ID, got arg error: %s", result.Stderr)
	}
}
