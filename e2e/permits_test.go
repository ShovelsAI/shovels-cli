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

// makePermitSearchHandler returns an HTTP handler that validates query
// parameters and serves paginated permit responses. totalItems controls
// the number of permits across all pages.
func makePermitSearchHandler(totalItems int, creditsUsed, creditsRemaining int) (http.Handler, *[]map[string][]string) {
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

func TestPermitsSearchBasic(t *testing.T) {
	handler, queries := makePermitSearchHandler(5, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--tags", "solar",
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
	if q["permit_tags"][0] != "solar" {
		t.Errorf("expected permit_tags=solar, got %q", q["permit_tags"])
	}
}

func TestPermitsSearchMultipleTags(t *testing.T) {
	handler, queries := makePermitSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--tags", "solar",
		"--tags", "roofing",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Verify both tags sent as repeated query params.
	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	tags := q["permit_tags"]
	if len(tags) != 2 {
		t.Fatalf("expected 2 permit_tags values, got %d: %v", len(tags), tags)
	}
	if tags[0] != "solar" || tags[1] != "roofing" {
		t.Errorf("expected tags [solar, roofing], got %v", tags)
	}
}

func TestPermitsSearchExclusionTag(t *testing.T) {
	handler, queries := makePermitSearchHandler(2, 2, 9998)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--tags", "solar",
		"--tags=-roofing",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Verify exclusion tag passed as-is with dash prefix.
	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	tags := q["permit_tags"]
	if len(tags) != 2 {
		t.Fatalf("expected 2 permit_tags values, got %d: %v", len(tags), tags)
	}
	if tags[0] != "solar" || tags[1] != "-roofing" {
		t.Errorf("expected tags [solar, -roofing], got %v", tags)
	}
}

// --- Edge cases ---

func TestPermitsSearchNoResults(t *testing.T) {
	handler, _ := makePermitSearchHandler(0, 0, 10000)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
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

func TestPermitsSearchOptionalFlagsOmitted(t *testing.T) {
	handler, queries := makePermitSearchHandler(10, 10, 9990)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Verify only required params and size were sent.
	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]

	// Should have geo_id, permit_from, permit_to, and size (added by paginator).
	// No optional params should be present.
	for _, optionalKey := range []string{
		"permit_tags", "permit_q", "permit_status", "permit_has_contractor",
		"property_type", "contractor_name",
	} {
		if _, exists := q[optionalKey]; exists {
			t.Errorf("optional param %q should not be sent when flag is omitted", optionalKey)
		}
	}

	parsed := parseEnvelope(t, result.Stdout)
	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 10 {
		t.Errorf("expected 10 items, got %d", len(data))
	}
}

// --- Error conditions ---

func TestPermitsSearchMissingRequiredFlags(t *testing.T) {
	env := withIsolatedConfig(t)

	// Missing all three required flags.
	result := runCLIWithEnv(t, env,
		"permits", "search",
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

func TestPermitsSearchMissingOneRequiredFlag(t *testing.T) {
	env := withIsolatedConfig(t)

	// Missing --geo-id only.
	result := runCLIWithEnv(t, env,
		"permits", "search",
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

func TestPermitsSearchInvalidDateFormat(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"permits", "search",
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

// --- Boundary conditions ---

func TestPermitsSearchQueryTooLong(t *testing.T) {
	env := withIsolatedConfig(t)

	// 51-character query exceeds the 50-char API maximum.
	longQuery := strings.Repeat("a", 51)

	result := runCLIWithEnv(t, env,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--query", longQuery,
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
	if !strings.Contains(p.Error, "50 characters") {
		t.Errorf("expected error about 50 character limit, got: %s", p.Error)
	}
	if !strings.Contains(p.Error, "51") {
		t.Errorf("expected error to mention actual length 51, got: %s", p.Error)
	}
}

func TestPermitsSearchInvalidStatus(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--status", "invalid_status",
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
	if !strings.Contains(p.Error, "invalid_status") {
		t.Errorf("expected error to mention the invalid value, got: %s", p.Error)
	}
	// Verify the error lists the valid options.
	for _, validStatus := range []string{"final", "in_review", "inactive", "active"} {
		if !strings.Contains(p.Error, validStatus) {
			t.Errorf("expected error to list valid option %q, got: %s", validStatus, p.Error)
		}
	}
}

func TestPermitsSearchRequiresAuth(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	result := runCLIWithEnv(t, env,
		"permits", "search",
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

func TestPermitsSearchPagination(t *testing.T) {
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
		"permits", "search",
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

func TestPermitsSearchQuery50CharsAccepted(t *testing.T) {
	handler, queries := makePermitSearchHandler(1, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	exactQuery := strings.Repeat("b", 50)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "90210",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--query", exactQuery,
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 for 50-char query, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Verify the query was sent to the API.
	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if q["permit_q"][0] != exactQuery {
		t.Errorf("expected permit_q=%q, got %q", exactQuery, q["permit_q"][0])
	}
}

// =======================================================================
// permits get
// =======================================================================

// makePermitGetHandler returns an HTTP handler that serves batch permit
// responses. knownIDs defines which IDs exist; unknown IDs are omitted
// from the response (the caller detects them as missing).
func makePermitGetHandler(knownIDs map[string]bool, creditsUsed, creditsRemaining int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := r.URL.Query()["id"]

		var items []json.RawMessage
		for _, id := range ids {
			if knownIDs[id] {
				items = append(items, json.RawMessage(fmt.Sprintf(
					`{"id":%q,"description":"Permit %s","status":"final"}`, id, id,
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

// --- permits get: Happy paths ---

func TestPermitsGetMultipleIDs(t *testing.T) {
	known := map[string]bool{"P123": true, "P456": true}
	srv := httptest.NewServer(makePermitGetHandler(known, 2, 9998))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "get", "P123", "P456",
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

// --- permits get: Edge cases ---

func TestPermitsGetSingleID(t *testing.T) {
	known := map[string]bool{"P123": true}
	srv := httptest.NewServer(makePermitGetHandler(known, 1, 9999))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "get", "P123",
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

	count := int(parsed.Meta["count"].(float64))
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}

	if _, ok := parsed.Meta["missing"]; ok {
		t.Error("expected missing to be absent when all IDs found")
	}
}

func TestPermitsGetSomeMissing(t *testing.T) {
	known := map[string]bool{"P123": true}
	srv := httptest.NewServer(makePermitGetHandler(known, 1, 9999))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "get", "P123", "P999", "P888",
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
		t.Errorf("expected 1 item in data (only P123 found), got %d", len(data))
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
	if missingArr[0].(string) != "P999" {
		t.Errorf("expected first missing ID P999, got %q", missingArr[0])
	}
	if missingArr[1].(string) != "P888" {
		t.Errorf("expected second missing ID P888, got %q", missingArr[1])
	}
}

// --- permits get: Error conditions ---

func TestPermitsGetNoIDs(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"permits", "get",
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
	if p.Error != "at least one permit ID required" {
		t.Errorf("expected error %q, got %q", "at least one permit ID required", p.Error)
	}
}

func TestPermitsGetTooManyIDs(t *testing.T) {
	env := withIsolatedConfig(t)

	args := []string{
		"permits", "get",
	}
	for i := range 51 {
		args = append(args, fmt.Sprintf("P%05d", i))
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

// --- permits get: Boundary conditions ---

func TestPermitsGetExactly50IDs(t *testing.T) {
	known := make(map[string]bool, 50)
	for i := range 50 {
		known[fmt.Sprintf("P%05d", i)] = true
	}

	srv := httptest.NewServer(makePermitGetHandler(known, 50, 9950))
	defer srv.Close()

	env := withIsolatedConfig(t)
	args := []string{
		"--base-url", srv.URL,
		"permits", "get",
	}
	for i := range 50 {
		args = append(args, fmt.Sprintf("P%05d", i))
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

func TestPermitsSearchInvalidDateFormatTo(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"permits", "search",
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

// --- include-count ---

func TestPermitsSearchIncludeCount(t *testing.T) {
	handler, queries := makePermitSearchHandler(25, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
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
	if int(tcMap["value"].(float64)) != 25 {
		t.Errorf("expected total_count.value=25, got %v", tcMap["value"])
	}
	if tcMap["relation"] != "eq" {
		t.Errorf("expected total_count.relation=eq, got %v", tcMap["relation"])
	}
}

func TestPermitsSearchNoIncludeCountByDefault(t *testing.T) {
	handler, queries := makePermitSearchHandler(5, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
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
