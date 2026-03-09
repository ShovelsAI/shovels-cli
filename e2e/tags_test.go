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

// makeTagListHandler returns an HTTP handler that serves paginated tag
// responses. Each tag has id and description fields. Unlike the geo search
// handlers, this handler respects the size query parameter, matching the real
// /list/tags endpoint which supports cursor/size pagination.
func makeTagListHandler(totalItems int, creditsUsed, creditsRemaining int) (http.Handler, *[]int) {
	var served atomic.Int32
	var expectedCursor atomic.Value
	requestedSizes := &[]int{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		if size == 0 {
			size = 50
		}
		*requestedSizes = append(*requestedSizes, size)

		// Validate cursor propagation.
		incomingCursor := r.URL.Query().Get("cursor")
		if want, ok := expectedCursor.Load().(string); ok && want != "" {
			if incomingCursor != want {
				w.WriteHeader(400)
				fmt.Fprintf(w, `{"error":"expected cursor %q, got %q"}`, want, incomingCursor)
				return
			}
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
				`{"id":"tag_%05d","description":"Description for tag %d"}`,
				start+i, start+i,
			))
		}

		var nextCursor *string
		if (start + count) < totalItems {
			c := fmt.Sprintf("cursor_%d", start+count)
			nextCursor = &c
			expectedCursor.Store(c)
		} else {
			expectedCursor.Store("")
		}

		w.Header().Set("X-Credits-Request", strconv.Itoa(creditsUsed))
		w.Header().Set("X-Credits-Remaining", strconv.Itoa(creditsRemaining))

		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: nextCursor}
		json.NewEncoder(w).Encode(resp)
	})

	return handler, requestedSizes
}

// --- Happy paths ---

func TestTagsListBasic(t *testing.T) {
	handler, sizes := makeTagListHandler(10, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"tags", "list",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []struct {
		ID          string `json:"id"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array of tag objects: %v", err)
	}
	if len(data) != 10 {
		t.Errorf("expected 10 items, got %d", len(data))
	}

	// Verify tag objects have required fields.
	for i, tag := range data {
		if tag.ID == "" {
			t.Errorf("data[%d]: id is empty", i)
		}
		if tag.Description == "" {
			t.Errorf("data[%d]: description is empty", i)
		}
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 10 {
		t.Errorf("expected count=10, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if hasMore {
		t.Error("expected has_more=false with only 10 items and default limit 50")
	}

	// Single API request for 10 items with default size=50.
	if len(*sizes) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*sizes))
	}
	if (*sizes)[0] != 50 {
		t.Errorf("expected request size=50, got %d", (*sizes)[0])
	}
}

func TestTagsListLimitAll(t *testing.T) {
	// 120 total items requires 2 pages at size=100.
	handler, sizes := makeTagListHandler(120, 10, 9880)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"--limit", "all",
		"tags", "list",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 120 {
		t.Errorf("expected 120 items, got %d", len(data))
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if hasMore {
		t.Error("expected has_more=false when all items fetched")
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 120 {
		t.Errorf("expected count=120, got %d", count)
	}

	// 120 items / 100 per page = 2 requests.
	if len(*sizes) != 2 {
		t.Fatalf("expected 2 API requests, got %d", len(*sizes))
	}
	expectedSizes := []int{100, 100}
	for i, want := range expectedSizes {
		if (*sizes)[i] != want {
			t.Errorf("request %d: expected size=%d, got %d", i+1, want, (*sizes)[i])
		}
	}
}

// --- Edge cases ---

func TestTagsListDefaultLimitWithHasMore(t *testing.T) {
	// Server has 200 tags. Default --limit 50 returns first page with has_more=true.
	handler, sizes := makeTagListHandler(200, 5, 9995)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"tags", "list",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 50 {
		t.Errorf("expected 50 items (default limit), got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 50 {
		t.Errorf("expected count=50, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if !hasMore {
		t.Error("expected has_more=true when more tags exist beyond default limit")
	}

	// Only 1 API request needed for default limit=50.
	if len(*sizes) != 1 {
		t.Errorf("expected 1 API request, got %d", len(*sizes))
	}
}

// --- Error conditions ---

func TestTagsListRequiresAuth(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	result := runCLIWithEnv(t, env,
		"tags", "list",
	)

	if result.ExitCode != 2 {
		t.Fatalf("expected exit 2 with no API key, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "auth_error" {
		t.Errorf("expected error_type %q, got %q", "auth_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "API key not configured") {
		t.Errorf("expected error about missing API key, got: %s", p.Error)
	}
}

// --- Boundary conditions ---

func TestTagsListMultiPagePagination(t *testing.T) {
	// 75 tags fit in a single request with page max of 100.
	handler, sizes := makeTagListHandler(75, 5, 9925)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"--limit", "all",
		"tags", "list",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 75 {
		t.Errorf("expected 75 items, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 75 {
		t.Errorf("expected count=75, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if hasMore {
		t.Error("expected has_more=false when all items fetched")
	}

	// 75 items in a single request (below page max of 100).
	if len(*sizes) != 1 {
		t.Fatalf("expected 1 API request, got %d", len(*sizes))
	}
	if (*sizes)[0] != 100 {
		t.Errorf("request 1: expected size=100, got %d", (*sizes)[0])
	}
}
