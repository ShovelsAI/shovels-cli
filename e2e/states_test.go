//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// makeStateSearchHandler returns an HTTP handler that validates the q query
// parameter and serves state responses. Each state has geo_id (abbreviation)
// and name (full state name). The handler ignores the size query parameter,
// matching the real /states/search endpoint.
func makeStateSearchHandler(totalItems int, creditsUsed, creditsRemaining int) (http.Handler, *[]map[string][]string) {
	capturedQueries := &[]map[string][]string{}

	// State abbreviations and names for test data.
	states := []struct {
		abbr string
		name string
	}{
		{"CA", "California"},
		{"CO", "Colorado"},
		{"CT", "Connecticut"},
		{"NC", "North Carolina"},
		{"SC", "South Carolina"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := map[string][]string{}
		for k, v := range r.URL.Query() {
			params[k] = v
		}
		*capturedQueries = append(*capturedQueries, params)

		if r.URL.Query().Get("q") == "" {
			w.WriteHeader(422)
			w.Write([]byte(`{"detail":"q is required"}`))
			return
		}

		items := make([]json.RawMessage, totalItems)
		for i := range totalItems {
			idx := i % len(states)
			items[i] = json.RawMessage(fmt.Sprintf(
				`{"geo_id":"%s","name":"%s"}`,
				states[idx].abbr, states[idx].name,
			))
		}

		w.Header().Set("X-Credits-Request", strconv.Itoa(creditsUsed))
		w.Header().Set("X-Credits-Remaining", strconv.Itoa(creditsRemaining))

		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: nil}
		json.NewEncoder(w).Encode(resp)
	})

	return handler, capturedQueries
}

// --- Happy paths ---

func TestStatesSearchBasic(t *testing.T) {
	handler, queries := makeStateSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"states", "search",
		"--query", "Cal",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []struct {
		GeoID string `json:"geo_id"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array of state objects: %v", err)
	}
	if len(data) != 3 {
		t.Errorf("expected 3 items, got %d", len(data))
	}

	// Verify state objects have required fields.
	for i, state := range data {
		if state.GeoID == "" {
			t.Errorf("data[%d]: geo_id is empty", i)
		}
		if state.Name == "" {
			t.Errorf("data[%d]: name is empty", i)
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

	// Verify query params sent to API.
	if len(*queries) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*queries))
	}
	q := (*queries)[0]
	if q["q"][0] != "Cal" {
		t.Errorf("expected q=%q, got %q", "Cal", q["q"][0])
	}
}

func TestStatesSearchWithLimit(t *testing.T) {
	handler, _ := makeStateSearchHandler(10, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"states", "search",
		"--query", "C",
		"--limit", "5",
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
		t.Errorf("expected exactly 5 items after client-side truncation, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 5 {
		t.Errorf("expected count=5, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if !hasMore {
		t.Error("expected has_more=true when server returned more items than the limit")
	}
}

// --- Edge cases ---

func TestStatesSearchNoResults(t *testing.T) {
	handler, _ := makeStateSearchHandler(0, 0, 10000)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"states", "search",
		"--query", "Zzyzx",
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

func TestStatesSearchShortQuery(t *testing.T) {
	handler, queries := makeStateSearchHandler(4, 4, 9996)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"states", "search",
		"-q", "C",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 4 {
		t.Errorf("expected 4 items for short query, got %d", len(data))
	}

	// Verify the short query was sent as-is.
	if len(*queries) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*queries))
	}
	if (*queries)[0]["q"][0] != "C" {
		t.Errorf("expected q=%q, got %q", "C", (*queries)[0]["q"][0])
	}
}

// --- Error conditions ---

func TestStatesSearchMissingQuery(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"states", "search",
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
	if !strings.Contains(p.Error, "required flag missing: --query (-q)") {
		t.Errorf("expected error to contain %q, got: %s", "required flag missing: --query (-q)", p.Error)
	}
}

func TestStatesSearchRequiresAuth(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	result := runCLIWithEnv(t, env,
		"states", "search",
		"--query", "Cal",
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

func TestStatesSearchHelpDescribesGeoID(t *testing.T) {
	result := runCLI(t, "states", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	if !strings.Contains(out, "--query") && !strings.Contains(out, "-q") {
		t.Error("states search --help should mention --query / -q flag")
	}
	if !strings.Contains(out, "(required)") {
		t.Error("states search --help should mark -q as required")
	}
	if !strings.Contains(out, "geo_id") || !strings.Contains(out, "--geo-id") {
		t.Error("states search --help should explain results are geo_ids for --geo-id")
	}
	if !strings.Contains(out, "abbreviation") {
		t.Error("states search --help should mention state abbreviations")
	}
}

func TestStatesSearchSinglePageNoPagination(t *testing.T) {
	handler, queries := makeStateSearchHandler(2, 2, 9998)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"states", "search",
		"-q", "New",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Only one API request should be made since next_cursor is null.
	if len(*queries) != 1 {
		t.Errorf("expected 1 API request (single page), got %d", len(*queries))
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 2 {
		t.Errorf("expected 2 items, got %d", len(data))
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if hasMore {
		t.Error("expected has_more=false for single-page result")
	}
}
