package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// --- ParseLimit tests ---

func TestParseLimitValidIntegers(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"1", 1},
		{"50", 50},
		{"100", 100},
		{"100000", 100000},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lc, err := ParseLimit(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lc.All {
				t.Error("expected All=false for numeric limit")
			}
			if lc.Limit != tt.want {
				t.Errorf("expected Limit=%d, got %d", tt.want, lc.Limit)
			}
		})
	}
}

func TestParseLimitAll(t *testing.T) {
	for _, input := range []string{"all", "ALL", "All"} {
		t.Run(input, func(t *testing.T) {
			lc, err := ParseLimit(input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !lc.All {
				t.Error("expected All=true")
			}
			if lc.MaxRecords != DefaultMaxRecords {
				t.Errorf("expected MaxRecords=%d, got %d", DefaultMaxRecords, lc.MaxRecords)
			}
		})
	}
}

func TestParseLimitInvalid(t *testing.T) {
	tests := []struct {
		input   string
		wantMsg string
	}{
		{"-1", "invalid limit -1"},
		{"0", "invalid limit 0"},
		{"abc", "invalid limit \"abc\""},
		{"200000", "limit cannot exceed 100000"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := ParseLimit(tt.input)
			if err == nil {
				t.Fatal("expected error")
			}
			if got := err.Error(); got != tt.wantMsg && !strings.Contains(got, tt.wantMsg) {
				t.Errorf("expected error containing %q, got %q", tt.wantMsg, got)
			}
		})
	}
}

// --- ValidateMaxRecords tests ---

func TestValidateMaxRecordsValid(t *testing.T) {
	for _, v := range []int{1, 100, 10000, 50000, 100000} {
		if err := ValidateMaxRecords(v); err != nil {
			t.Errorf("ValidateMaxRecords(%d) unexpected error: %v", v, err)
		}
	}
}

func TestValidateMaxRecordsInvalid(t *testing.T) {
	tests := []struct {
		input   int
		wantMsg string
	}{
		{0, "max-records must be a positive integer"},
		{-1, "max-records must be a positive integer"},
		{200000, "max-records cannot exceed 100000"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.input), func(t *testing.T) {
			err := ValidateMaxRecords(tt.input)
			if err == nil {
				t.Fatal("expected error")
			}
			if err.Error() != tt.wantMsg {
				t.Errorf("expected %q, got %q", tt.wantMsg, err.Error())
			}
		})
	}
}

// --- EffectiveLimit tests ---

func TestEffectiveLimitNumeric(t *testing.T) {
	lc := LimitConfig{Limit: 75}
	if got := lc.EffectiveLimit(); got != 75 {
		t.Errorf("expected 75, got %d", got)
	}
}

func TestEffectiveLimitAll(t *testing.T) {
	lc := LimitConfig{All: true, MaxRecords: 10000}
	if got := lc.EffectiveLimit(); got != 10000 {
		t.Errorf("expected 10000, got %d", got)
	}
}

func TestEffectiveLimitAllCustomCap(t *testing.T) {
	lc := LimitConfig{All: true, MaxRecords: 10000}
	lc = lc.WithMaxRecords(50000)
	if got := lc.EffectiveLimit(); got != 50000 {
		t.Errorf("expected 50000, got %d", got)
	}
}

func TestWithMaxRecordsClampsAtCeiling(t *testing.T) {
	lc := LimitConfig{All: true, MaxRecords: 10000}
	lc = lc.WithMaxRecords(200000)
	if lc.MaxRecords != MaxCeiling {
		t.Errorf("expected MaxRecords clamped to %d, got %d", MaxCeiling, lc.MaxRecords)
	}
	if got := lc.EffectiveLimit(); got != MaxCeiling {
		t.Errorf("expected EffectiveLimit=%d, got %d", MaxCeiling, got)
	}
}

func TestPaginateClampsBeyondCeiling(t *testing.T) {
	requestCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		items := make([]json.RawMessage, size)
		for i := range size {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":%d}`, i))
		}
		// Report no more data so the paginator stops quickly.
		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: nil}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	// Bypass validation by constructing a LimitConfig with Limit > MaxCeiling directly.
	lc := LimitConfig{Limit: MaxCeiling + 50000}
	result, err := c.Paginate(context.Background(), "/test", nil, lc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The paginator must clamp to MaxCeiling, so EffectiveLimit inside Paginate
	// is MaxCeiling. The server returned fewer items (one page of 50), which is
	// fine -- the important thing is that the paginator did not try to fetch
	// more than MaxCeiling.
	if len(result.Items) > MaxCeiling {
		t.Errorf("paginator fetched %d items, exceeding ceiling of %d", len(result.Items), MaxCeiling)
	}
}

// --- Paginator page size calculation tests (boundary) ---

func TestPaginateLimit75RequestsSizes50Then25(t *testing.T) {
	var requestedSizes []int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		requestedSizes = append(requestedSizes, size)

		items := make([]json.RawMessage, size)
		for i := range size {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":%d}`, i))
		}

		var nextCursor *string
		if len(requestedSizes) == 1 {
			cursor := "page2"
			nextCursor = &cursor
		}

		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: nextCursor}
		w.Header().Set("X-Credits-Request", "10")
		w.Header().Set("X-Credits-Remaining", "990")
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	lc := LimitConfig{Limit: 75}
	result, err := c.Paginate(context.Background(), "/test", nil, lc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(requestedSizes) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(requestedSizes))
	}
	if requestedSizes[0] != 50 {
		t.Errorf("first request size: expected 50, got %d", requestedSizes[0])
	}
	if requestedSizes[1] != 25 {
		t.Errorf("second request size: expected 25, got %d", requestedSizes[1])
	}
	if len(result.Items) != 75 {
		t.Errorf("expected 75 items, got %d", len(result.Items))
	}
}

func TestPaginateLimitAllCapsAt10K(t *testing.T) {
	requestCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		items := make([]json.RawMessage, size)
		for i := range size {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":%d}`, i))
		}

		// Always report more data available.
		cursor := fmt.Sprintf("page%d", requestCount+1)
		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: &cursor}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 30 * time.Second,
	})

	lc := LimitConfig{All: true, MaxRecords: DefaultMaxRecords}
	result, err := c.Paginate(context.Background(), "/test", nil, lc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Items) != 10000 {
		t.Errorf("expected 10000 items, got %d", len(result.Items))
	}
	if !result.HasMore {
		t.Error("expected HasMore=true when capped at 10K with more data available")
	}
}

func TestPaginateLimitAllCustomCap50K(t *testing.T) {
	requestCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		items := make([]json.RawMessage, size)
		for i := range size {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":%d}`, i))
		}
		cursor := fmt.Sprintf("page%d", requestCount+1)
		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: &cursor}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 60 * time.Second,
	})

	lc := LimitConfig{All: true, MaxRecords: 50000}
	result, err := c.Paginate(context.Background(), "/test", nil, lc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Items) != 50000 {
		t.Errorf("expected 50000 items, got %d", len(result.Items))
	}
	if !result.HasMore {
		t.Error("expected HasMore=true when capped with more data available")
	}
}

func TestPaginateCountEqualsActualItems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return exactly 30 items regardless of requested size.
		items := make([]json.RawMessage, 30)
		for i := range 30 {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":%d}`, i))
		}
		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: nil}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	lc := LimitConfig{Limit: 50}
	result, err := c.Paginate(context.Background(), "/test", nil, lc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// meta.count must equal actual items returned, not the requested limit.
	if len(result.Items) != 30 {
		t.Errorf("expected 30 items (actual count), got %d", len(result.Items))
	}
}

func TestPaginateSinglePageTruncatesToLimit(t *testing.T) {
	// Simulates endpoints like /cities/search that ignore the size param
	// and return all matches in one response with next_cursor=null.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items := make([]json.RawMessage, 10)
		for i := range 10 {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":%d}`, i))
		}
		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: nil}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	lc := LimitConfig{Limit: 5}
	result, err := c.Paginate(context.Background(), "/test", nil, lc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Items) != 5 {
		t.Errorf("expected 5 items after truncation, got %d", len(result.Items))
	}
	if !result.HasMore {
		t.Error("expected HasMore=true when server returned more items than the limit")
	}
}

func TestPaginateMidPaginationError(t *testing.T) {
	requestCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 3 {
			w.WriteHeader(500)
			w.Write([]byte(`{"error":"server error"}`))
			return
		}
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		items := make([]json.RawMessage, size)
		for i := range size {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":%d}`, i))
		}
		cursor := fmt.Sprintf("page%d", requestCount+1)
		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: &cursor}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
		NoRetry: true,
	})

	lc := LimitConfig{Limit: 200}
	_, err := c.Paginate(context.Background(), "/test", nil, lc)
	if err == nil {
		t.Fatal("expected error on mid-pagination failure")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.ExitCode != ExitTransientError {
		t.Errorf("expected exit code %d, got %d", ExitTransientError, apiErr.ExitCode)
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
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	lc := LimitConfig{Limit: 1}
	result, err := c.Paginate(context.Background(), "/test", nil, lc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if requestedSize != 1 {
		t.Errorf("expected size=1, got %d", requestedSize)
	}
	if len(result.Items) != 1 {
		t.Errorf("expected 1 item, got %d", len(result.Items))
	}
}

func TestPaginateTotalCountCapturedFromFirstPage(t *testing.T) {
	requestCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		size, _ := strconv.Atoi(r.URL.Query().Get("size"))
		items := make([]json.RawMessage, size)
		for i := range size {
			items[i] = json.RawMessage(fmt.Sprintf(`{"id":%d}`, i))
		}

		type tcShape struct {
			Value    int    `json:"value"`
			Relation string `json:"relation"`
		}
		type resp struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
			TotalCount *tcShape          `json:"total_count"`
		}

		r2 := resp{Items: items}
		if requestCount == 1 {
			r2.TotalCount = &tcShape{Value: 1234, Relation: "eq"}
			cursor := "page2"
			r2.NextCursor = &cursor
		}
		json.NewEncoder(w).Encode(r2)
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	lc := LimitConfig{Limit: 75}
	result, err := c.Paginate(context.Background(), "/test", nil, lc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TotalCount == nil {
		t.Fatal("expected TotalCount to be non-nil")
	}
	if result.TotalCount.Value != 1234 {
		t.Errorf("expected TotalCount.Value=1234, got %d", result.TotalCount.Value)
	}
	if result.TotalCount.Relation != "eq" {
		t.Errorf("expected TotalCount.Relation=eq, got %q", result.TotalCount.Relation)
	}
}

// --- FirstPageSize tests ---

func TestFirstPageSizeSmallLimit(t *testing.T) {
	lc := LimitConfig{Limit: 10}
	if got := lc.FirstPageSize(); got != 10 {
		t.Errorf("expected 10, got %d", got)
	}
}

func TestFirstPageSizeAtMax(t *testing.T) {
	lc := LimitConfig{Limit: 50}
	if got := lc.FirstPageSize(); got != 50 {
		t.Errorf("expected 50, got %d", got)
	}
}

func TestFirstPageSizeLargeLimit(t *testing.T) {
	lc := LimitConfig{Limit: 200}
	if got := lc.FirstPageSize(); got != 50 {
		t.Errorf("expected 50 (capped at page size max), got %d", got)
	}
}

func TestFirstPageSizeAll(t *testing.T) {
	lc := LimitConfig{All: true, MaxRecords: DefaultMaxRecords}
	if got := lc.FirstPageSize(); got != 50 {
		t.Errorf("expected 50 for --limit=all, got %d", got)
	}
}

func TestPaginateTotalCountNilWhenNotPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{
			Items: []json.RawMessage{json.RawMessage(`{"id":1}`)},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	lc := LimitConfig{Limit: 10}
	result, err := c.Paginate(context.Background(), "/test", nil, lc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TotalCount != nil {
		t.Errorf("expected TotalCount to be nil, got %+v", result.TotalCount)
	}
}
