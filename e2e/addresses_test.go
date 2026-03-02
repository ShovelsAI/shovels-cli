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

// makeAddressSearchHandler returns an HTTP handler that validates the q query
// parameter and serves paginated address responses. totalItems controls the
// number of addresses across all pages.
func makeAddressSearchHandler(totalItems int, creditsUsed, creditsRemaining int) (http.Handler, *[]map[string][]string) {
	var served atomic.Int32
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
				`{"id":"ADDR_%05d","address":"Address %d","city":"Test City","state":"CA","zip":"90210"}`,
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

func TestAddressesSearchBasic(t *testing.T) {
	handler, queries := makeAddressSearchHandler(3, 3, 9997)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"addresses", "search",
		"--query", "123 Main St",
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

	// Verify query params sent to API.
	if len(*queries) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*queries))
	}
	q := (*queries)[0]
	if q["q"][0] != "123 Main St" {
		t.Errorf("expected q=%q, got %q", "123 Main St", q["q"][0])
	}
}

func TestAddressesSearchShortFlag(t *testing.T) {
	handler, queries := makeAddressSearchHandler(2, 2, 9998)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"addresses", "search",
		"-q", "San Francisco",
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

	hasMore := parsed.Meta["has_more"].(bool)
	if hasMore {
		t.Error("expected has_more=false")
	}

	// Verify the short flag sent the correct query parameter.
	if len(*queries) < 1 {
		t.Fatal("expected at least 1 API request")
	}
	q := (*queries)[0]
	if q["q"][0] != "San Francisco" {
		t.Errorf("expected q=%q, got %q", "San Francisco", q["q"][0])
	}
}

// --- Edge cases ---

func TestAddressesSearchNoResults(t *testing.T) {
	handler, _ := makeAddressSearchHandler(0, 0, 10000)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"addresses", "search",
		"--query", "nonexistent address xyz",
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

func TestAddressesSearchMissingQuery(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env,
		"addresses", "search",
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
	if !strings.Contains(p.Error, "query is required") {
		t.Errorf("expected error to contain %q, got: %s", "query is required", p.Error)
	}
}

func TestAddressesSearchRequiresAuth(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	result := runCLIWithEnv(t, env,
		"addresses", "search",
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
