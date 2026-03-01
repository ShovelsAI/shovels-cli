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

// makePaginatedHandler returns an HTTP handler that serves paginated responses.
// totalItems controls the total number of items across all pages. Each page
// returns up to the requested size. If totalItems is -1, the handler always
// reports more data (infinite stream).
func makePaginatedHandler(totalItems int, creditsUsed, creditsRemaining int) http.Handler {
	var served atomic.Int32
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		if size == 0 {
			size = 50
		}

		start := int(served.Load())
		remaining := totalItems - start
		if totalItems < 0 {
			remaining = size // infinite mode
		}
		count := min(size, remaining)
		if count < 0 {
			count = 0
		}
		served.Add(int32(count))

		items := make([]json.RawMessage, count)
		for i := range count {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":%d}`, start+i))
		}

		var nextCursor *string
		moreExist := totalItems < 0 || (start+count) < totalItems
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

// --- Happy paths ---

func TestPaginateDefaultLimit50(t *testing.T) {
	srv := httptest.NewServer(makePaginatedHandler(200, 50, 9950))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"_test-paginate", "/search",
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
		t.Errorf("expected 50 items, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 50 {
		t.Errorf("expected count=50, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if !hasMore {
		t.Error("expected has_more=true with 200 total items")
	}

	cu := int(parsed.Meta["credits_used"].(float64))
	if cu != 50 {
		t.Errorf("expected credits_used=50, got %d", cu)
	}

	cr := int(parsed.Meta["credits_remaining"].(float64))
	if cr != 9950 {
		t.Errorf("expected credits_remaining=9950, got %d", cr)
	}
}

func TestPaginateLimit200MultiPage(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(requestCount.Add(1))
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))

		items := make([]json.RawMessage, size)
		for i := range size {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":%d}`, (n-1)*size+i))
		}

		// Always report more data.
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
		"--limit", "200",
		"_test-paginate", "/search",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	if len(data) != 200 {
		t.Errorf("expected 200 items, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 200 {
		t.Errorf("expected count=200, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if !hasMore {
		t.Error("expected has_more=true")
	}

	// 200 items / 50 per page = 4 requests
	if got := requestCount.Load(); got != 4 {
		t.Errorf("expected 4 API requests, got %d", got)
	}
}

func TestPaginateLimitAll(t *testing.T) {
	totalItems := 120
	srv := httptest.NewServer(makePaginatedHandler(totalItems, 10, 9880))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"--limit", "all",
		"_test-paginate", "/search",
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
}

// --- Edge cases ---

func TestPaginateLimit200ButOnly75Exist(t *testing.T) {
	srv := httptest.NewServer(makePaginatedHandler(75, 10, 990))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"--limit", "200",
		"_test-paginate", "/search",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	var data []json.RawMessage
	json.Unmarshal(parsed.Data, &data)

	if len(data) != 75 {
		t.Errorf("expected 75 items, got %d", len(data))
	}

	count := int(parsed.Meta["count"].(float64))
	if count != 75 {
		t.Errorf("expected count=75, got %d", count)
	}

	hasMore := parsed.Meta["has_more"].(bool)
	if hasMore {
		t.Error("expected has_more=false when fewer results than limit")
	}
}

func TestPaginateLimit1(t *testing.T) {
	var requestedSize int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedSize, _ = strconv.Atoi(r.URL.Query().Get("size"))
		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{
			Items:      []json.RawMessage{json.RawMessage(`{"id":1}`)},
			NextCursor: nil,
		}
		w.Header().Set("X-Credits-Request", "1")
		w.Header().Set("X-Credits-Remaining", "999")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"--limit", "1",
		"_test-paginate", "/search",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	var data []json.RawMessage
	json.Unmarshal(parsed.Data, &data)

	if len(data) != 1 {
		t.Errorf("expected 1 item, got %d", len(data))
	}
	if requestedSize != 1 {
		t.Errorf("expected API request with size=1, got %d", requestedSize)
	}
}

func TestVersionEnvelopeFormat(t *testing.T) {
	result := runCLI(t, "version")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", result.ExitCode)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// Data should be an object with version field.
	var data map[string]string
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data object: %v", err)
	}
	if data["version"] == "" {
		t.Error("expected version in data")
	}

	// Meta should be empty.
	if len(parsed.Meta) != 0 {
		t.Errorf("expected empty meta for version, got %v", parsed.Meta)
	}
}

// --- Error conditions ---

func TestPaginateLimitNegative(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--limit", "-1",
		"_test-paginate", "/search",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
}

func TestPaginateLimitZero(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--limit", "0",
		"_test-paginate", "/search",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
}

func TestPaginateLimitNonNumeric(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--limit", "abc",
		"_test-paginate", "/search",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if !strings.Contains(p.Error, "invalid limit") {
		t.Errorf("expected error about invalid limit, got: %s", p.Error)
	}
}

func TestPaginateLimitExceedsCeiling(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--limit", "200000",
		"_test-paginate", "/search",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if !strings.Contains(p.Error, "limit cannot exceed 100000") {
		t.Errorf("expected error about ceiling, got: %s", p.Error)
	}
}

func TestPaginateMaxRecordsExceedsCeiling(t *testing.T) {
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--limit", "all",
		"--max-records", "200000",
		"_test-paginate", "/search",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if !strings.Contains(p.Error, "max-records cannot exceed 100000") {
		t.Errorf("expected error about max-records ceiling, got: %s", p.Error)
	}
}

func TestPaginateMidPaginationErrorNoPartialOutput(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)

		if n == 3 {
			// Fail on page 3 of 5
			w.WriteHeader(500)
			w.Write([]byte(`{"error":"internal error"}`))
			return
		}

		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		items := make([]json.RawMessage, size)
		for i := range size {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":%d}`, i))
		}
		cursor := fmt.Sprintf("page%d", n+1)
		w.Header().Set("X-Credits-Request", "10")
		w.Header().Set("X-Credits-Remaining", "900")
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
		"--no-retry",
		"--limit", "250",
		"_test-paginate", "/search",
	)

	if result.ExitCode != 5 {
		t.Fatalf("expected exit 5 for server error, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Stdout should be empty -- no partial output.
	if strings.TrimSpace(result.Stdout) != "" {
		t.Errorf("expected empty stdout on mid-pagination error, got: %s", result.Stdout)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "server_error" {
		t.Errorf("expected error_type 'server_error', got %q", p.ErrorType)
	}
}

// --- Boundary conditions ---

func TestPaginateCountEqualsActualDataLength(t *testing.T) {
	// Server returns exactly 30 items, less than the default limit of 50.
	srv := httptest.NewServer(makePaginatedHandler(30, 5, 995))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"_test-paginate", "/search",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	var data []json.RawMessage
	json.Unmarshal(parsed.Data, &data)

	count := int(parsed.Meta["count"].(float64))
	if count != len(data) {
		t.Errorf("meta.count (%d) must equal len(data) (%d)", count, len(data))
	}
	if count != 30 {
		t.Errorf("expected 30, got %d", count)
	}
}
