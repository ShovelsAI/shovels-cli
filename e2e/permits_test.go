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

		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: nextCursor}
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
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
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
	if q["geo_id"][0] != "ZIP_90210" {
		t.Errorf("expected geo_id=ZIP_90210, got %q", q["geo_id"])
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
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
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
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
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
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "ZIP_99999",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
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
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
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
		"--api-key", "sk-test",
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
	if !strings.Contains(p.Error, "--from") {
		t.Errorf("expected error to mention --from, got: %s", p.Error)
	}
	if !strings.Contains(p.Error, "--to") {
		t.Errorf("expected error to mention --to, got: %s", p.Error)
	}
}

func TestPermitsSearchMissingOneRequiredFlag(t *testing.T) {
	env := withIsolatedConfig(t)

	// Missing --geo-id only.
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"permits", "search",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
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
		"--api-key", "sk-test",
		"permits", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024/01/01",
		"--to", "2024-12-31",
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
	if !strings.Contains(p.Error, "--from") {
		t.Errorf("expected error to mention --from, got: %s", p.Error)
	}
}

// --- Boundary conditions ---

func TestPermitsSearchQueryTooLong(t *testing.T) {
	env := withIsolatedConfig(t)

	// 51-character query exceeds the 50-char API maximum.
	longQuery := strings.Repeat("a", 51)

	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"permits", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
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
		"--api-key", "sk-test",
		"permits", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
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
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"permits", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
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
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"--limit", "100",
		"permits", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
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
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
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

func TestPermitsSearchInvalidDateFormatTo(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"permits", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024-01-01",
		"--to", "12-31-2024",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "--to") {
		t.Errorf("expected error to mention --to, got: %s", p.Error)
	}
}
