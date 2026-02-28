package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetrySucceedsOnSecondAttempt(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.Header().Set("X-Credits-Request", "10")
		w.Header().Set("X-Credits-Remaining", "990")
		w.WriteHeader(200)
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})
	// Override sleepFn to skip actual delays in tests.
	c.sleepFn = func(d time.Duration) {}

	resp, err := c.Get(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 calls (1 retry), got %d", calls.Load())
	}
	if string(resp.Body) != `{"items":[]}` {
		t.Errorf("expected successful body, got %q", string(resp.Body))
	}
}

func TestRetryAfterHeaderUsedAsDelay(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(429)
			w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	var sleepDuration time.Duration
	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 10 * time.Second,
	})
	c.sleepFn = func(d time.Duration) {
		sleepDuration = d
	}

	_, err := c.Get(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sleepDuration != 5*time.Second {
		t.Errorf("expected 5s delay from Retry-After header, got %v", sleepDuration)
	}
}

func TestRetryExhaustsMaxAttempts(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})
	c.sleepFn = func(d time.Duration) {}

	_, err := c.Get(context.Background(), "/test", nil)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.ExitCode != ExitRateLimit {
		t.Errorf("expected exit code %d, got %d", ExitRateLimit, apiErr.ExitCode)
	}
	if apiErr.ErrorType != ErrorTypeRateLimit {
		t.Errorf("expected error type %q, got %q", ErrorTypeRateLimit, apiErr.ErrorType)
	}
	// 1 initial + 3 retries = 4 total calls
	if calls.Load() != 4 {
		t.Errorf("expected 4 total calls (1 + 3 retries), got %d", calls.Load())
	}
}

func TestNoRetrySkipsRetries(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
		NoRetry: true,
	})
	c.sleepFn = func(d time.Duration) {}

	_, err := c.Get(context.Background(), "/test", nil)
	if err == nil {
		t.Fatal("expected error for 429 with no-retry")
	}

	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 call with no-retry, got %d", calls.Load())
	}
}

func TestAuthHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != "test-api-key" {
			t.Errorf("expected X-API-Key %q, got %q", "test-api-key", got)
		}
		if got := r.Header.Get("User-Agent"); got != "shovels-cli/1.0.0" {
			t.Errorf("expected User-Agent %q, got %q", "shovels-cli/1.0.0", got)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-api-key",
		BaseURL: srv.URL,
		Version: "1.0.0",
		Timeout: 5 * time.Second,
	})

	_, err := c.Get(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreditHeaderExtraction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Credits-Request", "42")
		w.Header().Set("X-Credits-Remaining", "958")
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	resp, err := c.Get(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Credits.CreditsUsed == nil || *resp.Credits.CreditsUsed != 42 {
		t.Errorf("expected credits_used=42, got %v", resp.Credits.CreditsUsed)
	}
	if resp.Credits.CreditsRemaining == nil || *resp.Credits.CreditsRemaining != 958 {
		t.Errorf("expected credits_remaining=958, got %v", resp.Credits.CreditsRemaining)
	}
}

func TestNoCreditHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	resp, err := c.Get(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Credits.CreditsUsed != nil {
		t.Errorf("expected nil credits_used for unlimited plan, got %d", *resp.Credits.CreditsUsed)
	}
	if resp.Credits.CreditsRemaining != nil {
		t.Errorf("expected nil credits_remaining for unlimited plan, got %d", *resp.Credits.CreditsRemaining)
	}
}

func TestErrorStatusCodes(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		exitCode  int
		errorType string
		wantMsg   string
	}{
		{
			name:      "401 unauthorized",
			status:    401,
			body:      `{"detail":"Invalid API key"}`,
			exitCode:  ExitAuthError,
			errorType: ErrorTypeAuth,
			wantMsg:   "Invalid API key",
		},
		{
			name:      "402 credit exhausted",
			status:    402,
			body:      `{"detail":"Credit limit exceeded"}`,
			exitCode:  ExitCreditExhausted,
			errorType: ErrorTypeCredit,
			wantMsg:   "Credit limit exceeded",
		},
		{
			name:      "422 validation error",
			status:    422,
			body:      `{"detail":[{"loc":["query","from"],"msg":"field required"}]}`,
			exitCode:  ExitClientError,
			errorType: ErrorTypeValidation,
		},
		{
			name:      "500 server error",
			status:    500,
			body:      `{"error":"internal error"}`,
			exitCode:  ExitTransientError,
			errorType: ErrorTypeServer,
			wantMsg:   "Server error",
		},
		{
			name:      "502 bad gateway",
			status:    502,
			body:      "",
			exitCode:  ExitTransientError,
			errorType: ErrorTypeServer,
			wantMsg:   "Server error",
		},
		{
			name:      "503 service unavailable",
			status:    503,
			body:      "",
			exitCode:  ExitTransientError,
			errorType: ErrorTypeServer,
			wantMsg:   "Server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			c := New(Options{
				APIKey:  "test-key",
				BaseURL: srv.URL,
				Timeout: 5 * time.Second,
				NoRetry: true,
			})

			_, err := c.Get(context.Background(), "/test", nil)
			if err == nil {
				t.Fatal("expected error")
			}

			apiErr, ok := err.(*APIError)
			if !ok {
				t.Fatalf("expected *APIError, got %T", err)
			}
			if apiErr.ExitCode != tt.exitCode {
				t.Errorf("expected exit code %d, got %d", tt.exitCode, apiErr.ExitCode)
			}
			if apiErr.ErrorType != tt.errorType {
				t.Errorf("expected error type %q, got %q", tt.errorType, apiErr.ErrorType)
			}
			if tt.wantMsg != "" && apiErr.Message != tt.wantMsg {
				t.Errorf("expected message %q, got %q", tt.wantMsg, apiErr.Message)
			}
		})
	}
}

func TestQueryParametersSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("geo_id"); got != "ZIP_90210" {
			t.Errorf("expected geo_id=ZIP_90210, got %q", got)
		}
		if got := r.URL.Query().Get("from"); got != "2024-01-01" {
			t.Errorf("expected from=2024-01-01, got %q", got)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 5 * time.Second,
	})

	_, err := c.Get(context.Background(), "/permits/search", map[string]string{
		"geo_id": "ZIP_90210",
		"from":   "2024-01-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRetryDelayComputedBackoff(t *testing.T) {
	// Without Retry-After, delays should be ~1s, ~2s, ~4s (with jitter).
	for attempt := range 3 {
		delay := retryDelay(attempt, http.Header{})
		baseMs := 1000.0 * float64(int(1)<<attempt) // 1000, 2000, 4000
		minMs := baseMs * 0.75
		maxMs := baseMs * 1.25

		ms := float64(delay.Milliseconds())
		if ms < minMs || ms > maxMs {
			t.Errorf("attempt %d: delay %v outside expected range [%v, %v]ms",
				attempt, delay, minMs, maxMs)
		}
	}
}

func TestRetryDelayWithRetryAfterHeader(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "7")

	delay := retryDelay(0, h)
	if delay != 7*time.Second {
		t.Errorf("expected 7s from Retry-After, got %v", delay)
	}
}

func TestExtractErrorMessageJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"error field", `{"error":"bad request"}`, "bad request"},
		{"detail string", `{"detail":"not found"}`, "not found"},
		{"detail array", `{"detail":[{"loc":["q"],"msg":"required"}]}`, `[{"loc":["q"],"msg":"required"}]`},
		{"empty body", "", ""},
		{"plain text", "Server Error", "Server Error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractErrorMessage([]byte(tt.body))
			if got != tt.want {
				t.Errorf("extractErrorMessage(%q) = %q, want %q", tt.body, got, tt.want)
			}
		})
	}
}

func TestContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(200)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := New(Options{
		APIKey:  "test-key",
		BaseURL: srv.URL,
		Timeout: 100 * time.Millisecond,
	})

	_, err := c.Get(context.Background(), "/test", nil)
	if err == nil {
		t.Fatal("expected timeout error")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.ExitCode != ExitTransientError {
		t.Errorf("expected exit code %d, got %d", ExitTransientError, apiErr.ExitCode)
	}
	if apiErr.ErrorType != ErrorTypeNetwork {
		t.Errorf("expected error type %q, got %q", ErrorTypeNetwork, apiErr.ErrorType)
	}
}
