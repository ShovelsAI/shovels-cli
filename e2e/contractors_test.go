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

// makeContractorSearchHandler returns an HTTP handler that validates query
// parameters and serves paginated contractor responses. totalItems controls
// the number of contractors across all pages.
func makeContractorSearchHandler(totalItems int, creditsUsed, creditsRemaining int) (http.Handler, *[]map[string][]string) {
	var served atomic.Int32
	capturedQueries := &[]map[string][]string{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture all query parameters for assertion.
		params := map[string][]string{}
		for k, v := range r.URL.Query() {
			params[k] = v
		}
		*capturedQueries = append(*capturedQueries, params)

		// Validate required params.
		if r.URL.Query().Get("geo_id") == "" {
			w.WriteHeader(422)
			w.Write([]byte(`{"detail":"geo_id is required"}`))
			return
		}
		if r.URL.Query().Get("permit_from") == "" {
			w.WriteHeader(422)
			w.Write([]byte(`{"detail":"permit_from is required"}`))
			return
		}
		if r.URL.Query().Get("permit_to") == "" {
			w.WriteHeader(422)
			w.Write([]byte(`{"detail":"permit_to is required"}`))
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
				`{"id":"C_%05d","name":"Contractor %d","classification":"general_building_contractor"}`,
				start+i, start+i,
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

		// Include total_count on first page when include_count=true.
		if r.URL.Query().Get("include_count") == "true" && r.URL.Query().Get("cursor") == "" {
			resp.TotalCount = &totalCountShape{Value: totalItems, Relation: "eq"}
		}

		json.NewEncoder(w).Encode(resp)
	})

	return handler, capturedQueries
}

// --- Happy paths ---

func TestContractorsSearchBasic(t *testing.T) {
	handler, queries := makeContractorSearchHandler(5, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 5 {
		t.Errorf("expected 5 items, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 5 {
		t.Errorf("expected count=5, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if hasMore {
		t.Error("expected has_more=false with only 5 items")
	}

	cu := int(parsed.Meta["credits_used"].(float64))
	if cu != 5 {
		t.Errorf("expected credits_used=5, got %d", cu)
	}

	cr := int(parsed.Meta["credits_remaining"].(float64))
	if cr != 9995 {
		t.Errorf("expected credits_remaining=9995, got %d", cr)
	}

	// Verify query params sent to API.
	if len(*queries) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*queries))
	}
	q := (*queries)[0]
	if q["geo_id"][0] != "90210" {
		t.Errorf("expected geo_id=90210, got %q", q["geo_id"])
	}
	if q["permit_from"][0] != "2024-01-01" {
		t.Errorf("expected permit_from=2024-01-01, got %q", q["permit_from"])
	}
	if q["permit_to"][0] != "2024-12-31" {
		t.Errorf("expected permit_to=2024-12-31, got %q", q["permit_to"])
	}
}

func TestContractorsSearchClassificationFilter(t *testing.T) {
	handler, queries := makeContractorSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--contractor-classification", "general_building_contractor",
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

	// Verify classification sent to API.
	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	cls := q["contractor_classification_derived"]
	if len(cls) != 1 || cls[0] != "general_building_contractor" {
		t.Errorf("expected contractor_classification_derived=[general_building_contractor], got %v", cls)
	}
}

// --- Edge cases ---

func TestContractorsSearchNoTallies(t *testing.T) {
	handler, queries := makeContractorSearchHandler(2, 2, 9998)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--no-tallies",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Verify include_tallies=false sent to API.
	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	it := q["include_tallies"]
	if len(it) != 1 || it[0] != "false" {
		t.Errorf("expected include_tallies=[false], got %v", it)
	}

	parsed := parseEnvelope(t, result.Stdout)
	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 2 {
		t.Errorf("expected 2 items, got %d", len(data))
	}
}

func TestContractorsSearchNoResults(t *testing.T) {
	handler, _ := makeContractorSearchHandler(0, 0, 10000)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "99999",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
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

func TestContractorsSearchNoTalliesOmittedByDefault(t *testing.T) {
	handler, queries := makeContractorSearchHandler(1, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Verify include_tallies is NOT sent when --no-tallies is omitted.
	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if _, exists := q["include_tallies"]; exists {
		t.Error("include_tallies should not be sent when --no-tallies is omitted")
	}
}

// --- Error conditions ---

func TestContractorsSearchMissingRequiredFlags(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"contractors", "search",
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
	if !strings.Contains(p.Error, "--geo-id") {
		t.Errorf("expected error to mention --geo-id, got: %s", p.Error)
	}
	if !strings.Contains(p.Error, "--permit-from") {
		t.Errorf("expected error to mention --permit-from, got: %s", p.Error)
	}
	if !strings.Contains(p.Error, "--permit-to") {
		t.Errorf("expected error to mention --permit-to, got: %s", p.Error)
	}
}

func TestContractorsSearchMissingOneRequiredFlag(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"contractors", "search",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
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

func TestContractorsSearchRequiresAuth(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	result := runCLIWithEnv(t, env,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
	)

	if result.ExitCode != 2 {
		t.Fatalf("expected exit 2 with no API key, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "auth_error" {
		t.Errorf("expected error_type %q, got %q", "auth_error", p.ErrorType)
	}
}

// --- Boundary conditions ---

func TestContractorsSearchInvalidDateFormat(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024/01/01",
		"--permit-to", "2024-12-31",
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
	if !strings.Contains(p.Error, "--permit-from") {
		t.Errorf("expected error to mention --permit-from, got: %s", p.Error)
	}
}

func TestContractorsSearchInvalidDateFormatTo(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "12-31-2024",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "--permit-to") {
		t.Errorf("expected error to mention --permit-to, got: %s", p.Error)
	}
}

func TestContractorsSearchPagination(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(requestCount.Add(1))
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))

		items := make([]json.RawMessage, size)
		for i := range size {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":"C_%05d"}`, (n-1)*size+i))
		}

		cursor := fmt.Sprintf("page%d", n+1)
		w.Header().Set("X-Credits-Request", "50")
		w.Header().Set("X-Credits-Remaining", "9800")
		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: &cursor}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"--limit", "100",
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 100 {
		t.Errorf("expected 100 items, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 100 {
		t.Errorf("expected count=100, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if !hasMore {
		t.Error("expected has_more=true")
	}

	// 100 items / 50 per page = 2 API requests.
	if got := requestCount.Load(); got != 2 {
		t.Errorf("expected 2 API requests, got %d", got)
	}
}

// --- include-count ---

func TestContractorsSearchIncludeCount(t *testing.T) {
	handler, queries := makeContractorSearchHandler(15, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--include-count",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Verify include_count=true sent to API.
	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	ic := q["include_count"]
	if len(ic) != 1 || ic[0] != "true" {
		t.Errorf("expected include_count=[true], got %v", ic)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// Verify total_count in meta.
	tcVal, ok := parsed.Meta["total_count"]
	if !ok {
		t.Fatal("expected total_count in meta when --include-count is set")
	}
	tcMap, ok := tcVal.(map[string]any)
	if !ok {
		t.Fatalf("expected total_count to be object, got %T", tcVal)
	}
	if int(tcMap["value"].(float64)) != 15 {
		t.Errorf("expected total_count.value=15, got %v", tcMap["value"])
	}
	if tcMap["relation"] != "eq" {
		t.Errorf("expected total_count.relation=eq, got %v", tcMap["relation"])
	}
}

func TestContractorsSearchNoIncludeCountByDefault(t *testing.T) {
	handler, queries := makeContractorSearchHandler(5, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Verify include_count is NOT sent when flag is omitted.
	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if _, exists := q["include_count"]; exists {
		t.Error("include_count should not be sent when --include-count is omitted")
	}

	parsed := parseEnvelope(t, result.Stdout)

	// Verify total_count is absent in meta.
	if _, ok := parsed.Meta["total_count"]; ok {
		t.Error("expected total_count to be absent when --include-count is not set")
	}
}

// =======================================================================
// contractors get
// =======================================================================

// makeContractorGetHandler returns an HTTP handler that serves batch
// contractor responses. knownIDs defines which IDs exist; unknown IDs
// are omitted from the response.
func makeContractorGetHandler(knownIDs map[string]bool, creditsUsed, creditsRemaining int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := r.URL.Query()["id"]

		var items []json.RawMessage
		for _, id := range ids {
			if knownIDs[id] {
				items = append(items, json.RawMessage(fmt.Sprintf(
					`{"id":%q,"name":"Contractor %s","classification":"general_building_contractor"}`, id, id,
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
}

// --- contractors get: Happy paths ---

func TestContractorsGetSingleID(t *testing.T) {
	known := map[string]bool{"C123": true}
	srv := httptest.NewServer(makeContractorGetHandler(known, 1, 9999))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "get", "C123",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// Single ID: data must be an object, not an array.
	var dataObj map[string]any
	if err := json.Unmarshal(parsed.Data, &dataObj); err != nil {
		t.Fatalf("expected data to be object for single ID, got: %s", string(parsed.Data))
	}
	if dataObj["id"] != "C123" {
		t.Errorf("expected id=C123, got %v", dataObj["id"])
	}
	if dataObj["name"] != "Contractor C123" {
		t.Errorf("expected name=%q, got %v", "Contractor C123", dataObj["name"])
	}

	// No count in meta for single-object responses.
	if _, ok := parsed.Meta["count"]; ok {
		t.Error("single-object response should not have count in meta")
	}

	// No has_more for batch (non-paginated) responses.
	if _, ok := parsed.Meta["has_more"]; ok {
		t.Error("batch response should not have has_more in meta")
	}

	// meta.missing should be absent when ID is found.
	if _, ok := parsed.Meta["missing"]; ok {
		t.Error("expected missing to be absent when ID is found")
	}

	cu := int(parsed.Meta["credits_used"].(float64))
	if cu != 1 {
		t.Errorf("expected credits_used=1, got %d", cu)
	}

	cr := int(parsed.Meta["credits_remaining"].(float64))
	if cr != 9999 {
		t.Errorf("expected credits_remaining=9999, got %d", cr)
	}
}

func TestContractorsGetMultipleIDs(t *testing.T) {
	known := map[string]bool{"C123": true, "C456": true}
	srv := httptest.NewServer(makeContractorGetHandler(known, 2, 9998))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "get", "C123", "C456",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// Multiple IDs: data must be an array.
	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array for multiple IDs: %v", err)
	}
	if len(data) != 2 {
		t.Errorf("expected 2 items, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 2 {
		t.Errorf("expected count=2, got %d", count)
	}

	// meta.missing should be absent when all IDs found.
	if _, ok := parsed.Meta["missing"]; ok {
		t.Error("expected missing to be absent when all IDs found")
	}

	// No has_more for batch (non-paginated) responses.
	if _, ok := parsed.Meta["has_more"]; ok {
		t.Error("batch response should not have has_more in meta")
	}

	cu := int(parsed.Meta["credits_used"].(float64))
	if cu != 2 {
		t.Errorf("expected credits_used=2, got %d", cu)
	}

	cr := int(parsed.Meta["credits_remaining"].(float64))
	if cr != 9998 {
		t.Errorf("expected credits_remaining=9998, got %d", cr)
	}
}

// --- contractors get: Edge cases ---

func TestContractorsGetSomeMissing(t *testing.T) {
	known := map[string]bool{"C123": true}
	srv := httptest.NewServer(makeContractorGetHandler(known, 1, 9999))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "get", "C123", "C999", "C888",
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
		t.Errorf("expected 1 item in data (only C123 found), got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
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
	if missingArr[0].(string) != "C999" {
		t.Errorf("expected first missing ID C999, got %q", missingArr[0])
	}
	if missingArr[1].(string) != "C888" {
		t.Errorf("expected second missing ID C888, got %q", missingArr[1])
	}
}

// --- contractors get: Error conditions ---

func TestContractorsGetNoIDs(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "get",
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
	if p.Error != "at least one contractor ID required" {
		t.Errorf("expected error %q, got %q", "at least one contractor ID required", p.Error)
	}
}

func TestContractorsGetTooManyIDs(t *testing.T) {
	env := withIsolatedConfig(t)

	args := []string{
		"contractors", "get",
	}
	for i := range 51 {
		args = append(args, fmt.Sprintf("C%05d", i))
	}

	result := runCLIWithEnv(t, env, args...)

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
	if p.Error != "maximum 50 IDs per request" {
		t.Errorf("expected error %q, got %q", "maximum 50 IDs per request", p.Error)
	}
}

// --- contractors get: Boundary conditions ---

func TestContractorsGetSingleIDIsObject(t *testing.T) {
	known := map[string]bool{"CSOLO": true}
	srv := httptest.NewServer(makeContractorGetHandler(known, 1, 9999))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "get", "CSOLO",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// Verify data is an object by attempting to unmarshal as a map.
	var dataObj map[string]any
	if err := json.Unmarshal(parsed.Data, &dataObj); err != nil {
		t.Fatalf("single ID: expected data to be object, got: %s", string(parsed.Data))
	}

	// Verify it is NOT an array by attempting to unmarshal as array.
	var dataArr []any
	if err := json.Unmarshal(parsed.Data, &dataArr); err == nil {
		t.Fatal("single ID: data should NOT be an array")
	}
}

func TestContractorsGetMultipleIDsIsArray(t *testing.T) {
	known := map[string]bool{"CA": true, "CB": true}
	srv := httptest.NewServer(makeContractorGetHandler(known, 2, 9998))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "get", "CA", "CB",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// Verify data is an array.
	var dataArr []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &dataArr); err != nil {
		t.Fatalf("multiple IDs: expected data to be array, got: %s", string(parsed.Data))
	}
	if len(dataArr) != 2 {
		t.Errorf("expected 2 items in array, got %d", len(dataArr))
	}
}

func TestContractorsGetExactly50IDs(t *testing.T) {
	known := make(map[string]bool, 50)
	for i := range 50 {
		known[fmt.Sprintf("C%05d", i)] = true
	}

	srv := httptest.NewServer(makeContractorGetHandler(known, 50, 9950))
	defer srv.Close()

	env := withIsolatedConfig(t)
	args := []string{
		"--base-url", srv.URL,
		"contractors", "get",
	}
	for i := range 50 {
		args = append(args, fmt.Sprintf("C%05d", i))
	}

	result := runCLIWithEnv(t, env, args...)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 50 {
		t.Errorf("expected 50 items, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 50 {
		t.Errorf("expected count=50, got %d", count)
	}

	if _, ok := parsed.Meta["missing"]; ok {
		t.Error("expected missing to be absent when all 50 IDs found")
	}
}

// =======================================================================
// contractors permits
// =======================================================================

// makeContractorPermitsHandler returns an HTTP handler that serves paginated
// permit responses for a specific contractor. The handler validates that the
// URL path contains the contractor ID.
func makeContractorPermitsHandler(totalItems int, creditsUsed, creditsRemaining int) (http.Handler, *[]map[string][]string) {
	var served atomic.Int32
	capturedQueries := &[]map[string][]string{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := map[string][]string{}
		for k, v := range r.URL.Query() {
			params[k] = v
		}
		*capturedQueries = append(*capturedQueries, params)

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
				`{"id":"P_%05d","description":"Permit %d","status":"final"}`,
				start+i, start+i,
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

		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: nextCursor}
		json.NewEncoder(w).Encode(resp)
	})

	return handler, capturedQueries
}

// --- contractors permits: Happy paths ---

func TestContractorsPermitsBasic(t *testing.T) {
	handler, _ := makeContractorPermitsHandler(5, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "permits", "ABC123",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 5 {
		t.Errorf("expected 5 items, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 5 {
		t.Errorf("expected count=5, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if hasMore {
		t.Error("expected has_more=false with only 5 items")
	}

	cu := int(parsed.Meta["credits_used"].(float64))
	if cu != 5 {
		t.Errorf("expected credits_used=5, got %d", cu)
	}

	cr := int(parsed.Meta["credits_remaining"].(float64))
	if cr != 9995 {
		t.Errorf("expected credits_remaining=9995, got %d", cr)
	}
}

// --- contractors permits: Edge cases ---

func TestContractorsPermitsNoResults(t *testing.T) {
	handler, _ := makeContractorPermitsHandler(0, 0, 10000)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "permits", "UNKNOWN_C",
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

	// meta.missing should be absent for paginated endpoints.
	if _, ok := parsed.Meta["missing"]; ok {
		t.Error("expected missing to be absent for paginated response")
	}
}

// --- contractors permits: Error conditions ---

func TestContractorsPermitsNoID(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "permits",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if !strings.Contains(p.Error, "accepts 1 arg(s)") {
		t.Errorf("expected error about arg count, got: %s", p.Error)
	}
}

// --- contractors permits: Boundary conditions ---

func TestContractorsPermitsExtraArgsRejected(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "permits", "ABC123", "EXTRA",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if !strings.Contains(p.Error, "accepts 1 arg(s)") {
		t.Errorf("expected error about arg count, got: %s", p.Error)
	}
}

func TestContractorsPermitsPagination(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(requestCount.Add(1))
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))

		items := make([]json.RawMessage, size)
		for i := range size {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":"P_%05d"}`, (n-1)*size+i))
		}

		cursor := fmt.Sprintf("page%d", n+1)
		w.Header().Set("X-Credits-Request", "50")
		w.Header().Set("X-Credits-Remaining", "9800")
		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: &cursor}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"--limit", "100",
		"contractors", "permits", "ABC123",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 100 {
		t.Errorf("expected 100 items, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 100 {
		t.Errorf("expected count=100, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if !hasMore {
		t.Error("expected has_more=true")
	}

	if got := requestCount.Load(); got != 2 {
		t.Errorf("expected 2 API requests, got %d", got)
	}
}

// =======================================================================
// contractors employees
// =======================================================================

// makeContractorEmployeesHandler returns an HTTP handler that serves paginated
// employee responses for a specific contractor.
func makeContractorEmployeesHandler(totalItems int, creditsUsed, creditsRemaining int) http.Handler {
	var served atomic.Int32

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
				`{"name":"Employee %d","title":"Engineer"}`,
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

		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: nextCursor}
		json.NewEncoder(w).Encode(resp)
	})
}

// --- contractors employees: Happy paths ---

func TestContractorsEmployeesBasic(t *testing.T) {
	handler := makeContractorEmployeesHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "employees", "ABC123",
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

	count := int(parsed.Meta["count"].(float64))
	if count != 3 {
		t.Errorf("expected count=3, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if hasMore {
		t.Error("expected has_more=false with only 3 items")
	}

	cu := int(parsed.Meta["credits_used"].(float64))
	if cu != 3 {
		t.Errorf("expected credits_used=3, got %d", cu)
	}

	cr := int(parsed.Meta["credits_remaining"].(float64))
	if cr != 9997 {
		t.Errorf("expected credits_remaining=9997, got %d", cr)
	}
}

// --- contractors employees: Error conditions ---

func TestContractorsEmployeesNoID(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "employees",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if !strings.Contains(p.Error, "accepts 1 arg(s)") {
		t.Errorf("expected error about arg count, got: %s", p.Error)
	}
}

func TestContractorsEmployeesExtraArgsRejected(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "employees", "ABC123", "EXTRA",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if !strings.Contains(p.Error, "accepts 1 arg(s)") {
		t.Errorf("expected error about arg count, got: %s", p.Error)
	}
}

// =======================================================================
// contractors metrics
// =======================================================================

// makeContractorMetricsHandler returns an HTTP handler that validates required
// query parameters and serves monthly metrics for a contractor.
func makeContractorMetricsHandler(creditsUsed, creditsRemaining int) (http.Handler, *[]map[string][]string) {
	capturedQueries := &[]map[string][]string{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := map[string][]string{}
		for k, v := range r.URL.Query() {
			params[k] = v
		}
		*capturedQueries = append(*capturedQueries, params)

		w.Header().Set("X-Credits-Request", strconv.Itoa(creditsUsed))
		w.Header().Set("X-Credits-Remaining", strconv.Itoa(creditsRemaining))

		items := []json.RawMessage{
			json.RawMessage(`{"month":"2024-01","permit_count":10,"avg_job_value":50000}`),
			json.RawMessage(`{"month":"2024-02","permit_count":8,"avg_job_value":45000}`),
			json.RawMessage(`{"month":"2024-03","permit_count":12,"avg_job_value":55000}`),
		}

		resp := struct {
			Items []json.RawMessage `json:"items"`
		}{Items: items}
		json.NewEncoder(w).Encode(resp)
	})

	return handler, capturedQueries
}

// --- contractors metrics: Happy paths ---

func TestContractorsMetricsBasic(t *testing.T) {
	handler, queries := makeContractorMetricsHandler(10, 9990)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "metrics", "ABC123",
		"--metric-from", "2024-01-01",
		"--metric-to", "2024-12-31",
		"--property-type", "residential",
		"--tag", "solar",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// Data should be an array of monthly metrics.
	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 3 {
		t.Errorf("expected 3 monthly metrics, got %d", len(data))
	}

	// Verify first metric has expected shape.
	var firstMetric map[string]any
	if err := json.Unmarshal(data[0], &firstMetric); err != nil {
		t.Fatalf("failed to parse first metric: %v", err)
	}
	if firstMetric["month"] != "2024-01" {
		t.Errorf("expected month=2024-01, got %v", firstMetric["month"])
	}
	if int(firstMetric["permit_count"].(float64)) != 10 {
		t.Errorf("expected permit_count=10, got %v", firstMetric["permit_count"])
	}

	// Non-paginated: no count or has_more in meta.
	if _, exists := parsed.Meta["count"]; exists {
		t.Error("metrics response must not contain count in meta")
	}
	if _, exists := parsed.Meta["has_more"]; exists {
		t.Error("metrics response must not contain has_more in meta")
	}

	cu := int(parsed.Meta["credits_used"].(float64))
	if cu != 10 {
		t.Errorf("expected credits_used=10, got %d", cu)
	}

	cr := int(parsed.Meta["credits_remaining"].(float64))
	if cr != 9990 {
		t.Errorf("expected credits_remaining=9990, got %d", cr)
	}

	// Verify query params sent to API.
	if len(*queries) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*queries))
	}
	q := (*queries)[0]
	if q["metric_from"][0] != "2024-01-01" {
		t.Errorf("expected metric_from=2024-01-01, got %q", q["metric_from"])
	}
	if q["metric_to"][0] != "2024-12-31" {
		t.Errorf("expected metric_to=2024-12-31, got %q", q["metric_to"])
	}
	if q["property_type"][0] != "residential" {
		t.Errorf("expected property_type=residential, got %q", q["property_type"])
	}
	if q["tag"][0] != "solar" {
		t.Errorf("expected tag=solar, got %q", q["tag"])
	}
}

// --- contractors metrics: Error conditions ---

func TestContractorsMetricsMissingAllFlags(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "metrics", "ABC123",
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
	if !strings.Contains(p.Error, "--property-type") {
		t.Errorf("expected error to mention --property-type, got: %s", p.Error)
	}
	if !strings.Contains(p.Error, "--tag") {
		t.Errorf("expected error to mention --tag, got: %s", p.Error)
	}
}

func TestContractorsMetricsMissingSomeFlags(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "metrics", "ABC123",
		"--metric-from", "2024-01-01",
		"--metric-to", "2024-12-31",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	// Should mention the missing flags but not the provided ones.
	if !strings.Contains(p.Error, "--property-type") {
		t.Errorf("expected error to mention --property-type, got: %s", p.Error)
	}
	if !strings.Contains(p.Error, "--tag") {
		t.Errorf("expected error to mention --tag, got: %s", p.Error)
	}
	if strings.Contains(p.Error, "--metric-from") {
		t.Errorf("expected error NOT to mention --metric-from (it was provided), got: %s", p.Error)
	}
}

func TestContractorsMetricsNoID(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "metrics",
		"--metric-from", "2024-01-01",
		"--metric-to", "2024-12-31",
		"--property-type", "residential",
		"--tag", "solar",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if !strings.Contains(p.Error, "accepts 1 arg(s)") {
		t.Errorf("expected error about arg count, got: %s", p.Error)
	}
}

// --- contractors metrics: Boundary conditions ---

func TestContractorsMetricsExactlyOneIDAccepted(t *testing.T) {
	handler, _ := makeContractorMetricsHandler(5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "metrics", "SINGLE_ID",
		"--metric-from", "2024-01-01",
		"--metric-to", "2024-12-31",
		"--property-type", "residential",
		"--tag", "solar",
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
		t.Errorf("expected 3 monthly metrics, got %d", len(data))
	}
}

func TestContractorsMetricsExtraArgsRejected(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"contractors", "metrics", "ABC123", "EXTRA",
		"--metric-from", "2024-01-01",
		"--metric-to", "2024-12-31",
		"--property-type", "residential",
		"--tag", "solar",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if !strings.Contains(p.Error, "accepts 1 arg(s)") {
		t.Errorf("expected error about arg count, got: %s", p.Error)
	}
}
