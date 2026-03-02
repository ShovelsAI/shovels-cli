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

// makeCitySearchHandler returns an HTTP handler that validates the q query
// parameter and serves city responses. Each city has geo_id, name, and state.
// The handler ignores the size query parameter, matching the real /cities/search
// endpoint which returns all matches in a single response regardless of size.
func makeCitySearchHandler(totalItems int, creditsUsed, creditsRemaining int) (http.Handler, *[]map[string][]string) {
	capturedQueries := &[]map[string][]string{}

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

		// The real cities endpoint ignores size and returns all matches.
		items := make([]json.RawMessage, totalItems)
		for i := range totalItems {
			items[i] = json.RawMessage(fmt.Sprintf(
				`{"geo_id":"city_%05d","name":"CITY %d, STATE, ST","state":"ST"}`,
				i, i,
			))
		}

		w.Header().Set("X-Credits-Request", strconv.Itoa(creditsUsed))
		w.Header().Set("X-Credits-Remaining", strconv.Itoa(creditsRemaining))

		// Single-page response: next_cursor is null.
		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: nil}
		json.NewEncoder(w).Encode(resp)
	})

	return handler, capturedQueries
}

// --- Happy paths ---

func TestCitiesSearchBasic(t *testing.T) {
	handler, queries := makeCitySearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "search",
		"--query", "Miami",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []struct {
		GeoID string `json:"geo_id"`
		Name  string `json:"name"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array of city objects: %v", err)
	}
	if len(data) != 3 {
		t.Errorf("expected 3 items, got %d", len(data))
	}

	// Verify city objects have required fields.
	for i, city := range data {
		if city.GeoID == "" {
			t.Errorf("data[%d]: geo_id is empty", i)
		}
		if city.Name == "" {
			t.Errorf("data[%d]: name is empty", i)
		}
		if city.State == "" {
			t.Errorf("data[%d]: state is empty", i)
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
	if q["q"][0] != "Miami" {
		t.Errorf("expected q=%q, got %q", "Miami", q["q"][0])
	}
}

func TestCitiesSearchWithLimit(t *testing.T) {
	// Server returns 10 items (ignoring size param, like the real endpoint).
	// --limit 5 must truncate client-side to exactly 5.
	handler, _ := makeCitySearchHandler(10, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "search",
		"--query", "San Francisco",
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

func TestCitiesSearchMultipleResults(t *testing.T) {
	handler, _ := makeCitySearchHandler(5, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "search",
		"-q", "Portland",
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
		t.Errorf("expected 5 items for ambiguous query, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 5 {
		t.Errorf("expected count=5, got %d", count)
	}
}

func TestCitiesSearchNoResults(t *testing.T) {
	handler, _ := makeCitySearchHandler(0, 0, 10000)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "search",
		"--query", "nonexistent city xyz",
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

// --- Error conditions ---

func TestCitiesSearchMissingQuery(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"cities", "search",
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

func TestCitiesSearchRequiresAuth(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	result := runCLIWithEnv(t, env,
		"cities", "search",
		"--query", "test",
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

func TestCitiesSearchSinglePageNoPagination(t *testing.T) {
	// The cities API returns no pagination params. cl.Paginate() handles this
	// by stopping when next_cursor is null, producing a single-page result.
	handler, queries := makeCitySearchHandler(2, 2, 9998)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "search",
		"-q", "Test City",
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
