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
				`{"id":"C_%05d","name":"Contractor %d","classification":"general_building"}`,
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

func TestContractorsSearchBasic(t *testing.T) {
	handler, queries := makeContractorSearchHandler(5, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"contractors", "search",
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
}

func TestContractorsSearchClassificationFilter(t *testing.T) {
	handler, queries := makeContractorSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
		"--contractor-classification", "general_building",
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
	if len(cls) != 1 || cls[0] != "general_building" {
		t.Errorf("expected contractor_classification_derived=[general_building], got %v", cls)
	}
}

// --- Edge cases ---

func TestContractorsSearchNoTallies(t *testing.T) {
	handler, queries := makeContractorSearchHandler(2, 2, 9998)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
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
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"contractors", "search",
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

func TestContractorsSearchNoTalliesOmittedByDefault(t *testing.T) {
	handler, queries := makeContractorSearchHandler(1, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"contractors", "search",
		"--geo-id", "ZIP_90210",
		"--from", "2024-01-01",
		"--to", "2024-12-31",
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
		"--api-key", "sk-test",
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
	if !strings.Contains(p.Error, "--from") {
		t.Errorf("expected error to mention --from, got: %s", p.Error)
	}
	if !strings.Contains(p.Error, "--to") {
		t.Errorf("expected error to mention --to, got: %s", p.Error)
	}
}

func TestContractorsSearchMissingOneRequiredFlag(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"contractors", "search",
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

func TestContractorsSearchRequiresAuth(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"contractors", "search",
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

// --- Boundary conditions ---

func TestContractorsSearchInvalidDateFormat(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"contractors", "search",
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

func TestContractorsSearchInvalidDateFormatTo(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"contractors", "search",
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
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"--limit", "100",
		"contractors", "search",
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
