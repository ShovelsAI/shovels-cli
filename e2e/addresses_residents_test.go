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

// makeResidentsHandler returns an HTTP handler that serves paginated resident
// responses for an address geo_id. totalItems controls the number of residents
// across all pages.
func makeResidentsHandler(totalItems int, creditsUsed, creditsRemaining int) (http.Handler, *[]map[string][]string) {
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
				`{"name":"Resident %d","personal_emails":["r%d@test.com"],"phone":"555-0%03d","linkedin_url":"https://linkedin.com/in/r%d","net_worth":"1M-5M","income_range":"100K-150K","is_homeowner":true,"street_no":"%d","street":"Main St","city":"Miami","state":"FL","zip_code":"33101","zip_code_ext":"1234"}`,
				start+i, start+i, start+i, start+i, 100+start+i,
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

// makeResidentsNullFieldsHandler returns a handler that serves a single resident
// record with all fields set to null.
func makeResidentsNullFieldsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Credits-Request", "1")
		w.Header().Set("X-Credits-Remaining", "9999")

		resp := `{"items":[{"name":null,"personal_emails":null,"phone":null,"linkedin_url":null,"net_worth":null,"income_range":null,"is_homeowner":null,"street_no":null,"street":null,"city":null,"state":null,"zip_code":null,"zip_code_ext":null}],"next_cursor":null}`
		w.Write([]byte(resp))
	})
}

// --- Happy paths ---

func TestAddressesResidentsBasic(t *testing.T) {
	handler, _ := makeResidentsHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"addresses", "residents", "ADDR_ABC123",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []map[string]any
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 3 {
		t.Errorf("expected 3 items, got %d", len(data))
	}

	// Verify resident record fields are present.
	first := data[0]
	expectedFields := []string{
		"name", "personal_emails", "phone", "linkedin_url",
		"net_worth", "income_range", "is_homeowner",
		"street_no", "street", "city", "state", "zip_code", "zip_code_ext",
	}
	for _, field := range expectedFields {
		if _, ok := first[field]; !ok {
			t.Errorf("expected field %q in resident record", field)
		}
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

func TestAddressesResidentsWithLimit(t *testing.T) {
	handler, _ := makeResidentsHandler(20, 10, 9990)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"addresses", "residents", "ADDR_XYZ",
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
		t.Errorf("expected 10 items (limited), got %d", len(data))
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if !hasMore {
		t.Error("expected has_more=true with 20 total items and limit 10")
	}
}

// --- Edge cases ---

func TestAddressesResidentsEmpty(t *testing.T) {
	handler, _ := makeResidentsHandler(0, 0, 10000)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"addresses", "residents", "ADDR_EMPTY",
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

func TestAddressesResidentsNullFields(t *testing.T) {
	srv := httptest.NewServer(makeResidentsNullFieldsHandler())
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"addresses", "residents", "ADDR_NULLS",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []map[string]*json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 item with null fields, got %d", len(data))
	}

	// Verify the record appears with null values rather than being filtered out.
	record := data[0]
	nullFields := []string{
		"name", "personal_emails", "phone", "linkedin_url",
		"net_worth", "income_range", "is_homeowner",
		"street_no", "street", "city", "state", "zip_code", "zip_code_ext",
	}
	for _, field := range nullFields {
		val, ok := record[field]
		if !ok {
			t.Errorf("expected field %q to be present (even if null)", field)
			continue
		}
		if val != nil {
			var raw string
			if err := json.Unmarshal(*val, &raw); err == nil && raw != "" {
				t.Errorf("expected field %q to be null, got %s", field, string(*val))
			}
		}
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
}

// --- Error conditions ---

func TestAddressesResidentsMissingGeoID(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"addresses", "residents",
	)

	if result.ExitCode == 0 {
		t.Fatal("expected non-zero exit code for missing geo_id")
	}

	// Cobra emits its own error for missing positional args.
	if !strings.Contains(result.Stderr, "accepts 1 arg(s)") {
		t.Errorf("expected cobra arg count error, got stderr: %s", result.Stderr)
	}
}

func TestAddressesResidentsInvalidGeoID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		w.Write([]byte(`{"detail":"Invalid geo_id"}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"addresses", "residents", "INVALID_GEO",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
}

func TestAddressesResidentsRequiresAuth(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	result := runCLIWithEnv(t, env,
		"addresses", "residents", "ADDR_ABC",
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

func TestAddressesResidentsHelpDocsPII(t *testing.T) {
	result := runCLI(t, "addresses", "residents", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	helpText := result.Stdout

	// Help text must document the personal information nature of the data.
	piiTerms := []string{
		"personal information",
		"name",
		"email",
		"phone",
		"LinkedIn",
		"income",
		"homeowner",
	}
	for _, term := range piiTerms {
		if !strings.Contains(strings.ToLower(helpText), strings.ToLower(term)) {
			t.Errorf("help text should mention %q to document PII nature", term)
		}
	}

	// Help text must show workflow for resolving address first.
	if !strings.Contains(helpText, "addresses search") {
		t.Error("help text should show workflow using 'addresses search' to resolve geo_id")
	}
}

func TestAddressesResidentsHelpShowsWorkflow(t *testing.T) {
	result := runCLI(t, "addresses", "residents", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Verify the help text includes the address-first resolution pattern.
	if !strings.Contains(result.Stdout, "addresses search -q") {
		t.Error("help text should show 'addresses search -q' workflow for geo_id resolution")
	}
}

func TestAddressesParentHelpListsResidents(t *testing.T) {
	result := runCLI(t, "addresses", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if !strings.Contains(result.Stdout, "residents") {
		t.Error("addresses --help should list 'residents' subcommand")
	}
}
