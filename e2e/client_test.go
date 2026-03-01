//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// errorPayload mirrors the structured JSON error written to stderr.
type errorPayload struct {
	Error     string `json:"error"`
	Code      int    `json:"code"`
	ErrorType string `json:"error_type"`
}

// envelope mirrors the standard CLI response envelope.
type envelope struct {
	Data json.RawMessage `json:"data"`
	Meta map[string]any  `json:"meta"`
}

func parseStderrError(t *testing.T, stderr string) errorPayload {
	t.Helper()
	var p errorPayload
	if err := json.Unmarshal([]byte(stderr), &p); err != nil {
		t.Fatalf("stderr is not valid JSON: %v\nstderr: %s", err, stderr)
	}
	return p
}

func parseEnvelope(t *testing.T, stdout string) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, stdout)
	}
	return env
}

// --- Happy paths ---

func TestHTTPClientSendsAuthHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		userAgent := r.Header.Get("User-Agent")

		w.WriteHeader(200)
		fmt.Fprintf(w, `{"api_key":"%s","user_agent":"%s"}`, apiKey, userAgent)
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test-header-key",
		"--base-url", srv.URL,
		"_test-http", "/check-headers",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	var data struct {
		APIKey    string `json:"api_key"`
		UserAgent string `json:"user_agent"`
	}
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("failed to parse data: %v", err)
	}

	if data.APIKey != "sk-test-header-key" {
		t.Errorf("expected X-API-Key %q, got %q", "sk-test-header-key", data.APIKey)
	}
	if !strings.HasPrefix(data.UserAgent, "shovels-cli/") {
		t.Errorf("expected User-Agent starting with 'shovels-cli/', got %q", data.UserAgent)
	}
}

func TestHTTPClientExtractsCreditHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Credits-Request", "10")
		w.Header().Set("X-Credits-Limit", "1000")
		w.Header().Set("X-Credits-Remaining", "990")
		w.WriteHeader(200)
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"_test-http", "/test",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	creditsUsed, ok := parsed.Meta["credits_used"]
	if !ok {
		t.Fatal("expected credits_used in meta")
	}
	if int(creditsUsed.(float64)) != 10 {
		t.Errorf("expected credits_used=10, got %v", creditsUsed)
	}

	creditsLimit, ok := parsed.Meta["credits_limit"]
	if !ok {
		t.Fatal("expected credits_limit in meta")
	}
	if int(creditsLimit.(float64)) != 1000 {
		t.Errorf("expected credits_limit=1000, got %v", creditsLimit)
	}

	creditsRemaining, ok := parsed.Meta["credits_remaining"]
	if !ok {
		t.Fatal("expected credits_remaining in meta")
	}
	if int(creditsRemaining.(float64)) != 990 {
		t.Errorf("expected credits_remaining=990, got %v", creditsRemaining)
	}
}

// --- Edge cases ---

func TestHTTPClientNoCreditHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"_test-http", "/test",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	cr, ok := parsed.Meta["credits_remaining"]
	if !ok {
		t.Fatal("expected credits_remaining to be present in meta for unlimited plan")
	}
	if cr != nil {
		t.Errorf("expected credits_remaining to be null for unlimited plan, got %v", cr)
	}
	cl, ok := parsed.Meta["credits_limit"]
	if !ok {
		t.Fatal("expected credits_limit to be present in meta for unlimited plan")
	}
	if cl != nil {
		t.Errorf("expected credits_limit to be null for unlimited plan, got %v", cl)
	}
	if _, ok := parsed.Meta["credits_used"]; ok {
		t.Error("expected credits_used to be absent in meta when no credit headers present")
	}
}

func TestHTTPClientEmptyItemsResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Credits-Request", "0")
		w.Header().Set("X-Credits-Remaining", "1000")
		w.WriteHeader(200)
		w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"_test-http", "/test",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)
	var data struct {
		Items []any `json:"items"`
	}
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("failed to parse data: %v", err)
	}
	if len(data.Items) != 0 {
		t.Errorf("expected empty items array, got %d items", len(data.Items))
	}
}

// --- Error conditions ---

func TestHTTPClient401ExitsCode2(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"detail":"Invalid API key"}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-bad",
		"--base-url", srv.URL,
		"_test-http", "/test",
	)

	if result.ExitCode != 2 {
		t.Fatalf("expected exit 2, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 2 {
		t.Errorf("expected error code 2, got %d", p.Code)
	}
	if p.ErrorType != "auth_error" {
		t.Errorf("expected error_type %q, got %q", "auth_error", p.ErrorType)
	}
}

func TestHTTPClient402ExitsCode4(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(402)
		w.Write([]byte(`{"detail":"Credit limit exceeded"}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"_test-http", "/test",
	)

	if result.ExitCode != 4 {
		t.Fatalf("expected exit 4, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 4 {
		t.Errorf("expected error code 4, got %d", p.Code)
	}
	if p.ErrorType != "credit_exhausted" {
		t.Errorf("expected error_type %q, got %q", "credit_exhausted", p.ErrorType)
	}
	if !strings.Contains(p.Error, "Credit limit exceeded") {
		t.Errorf("expected error message about credit limit, got: %s", p.Error)
	}
}

func TestHTTPClient422ExitsCode1(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		w.Write([]byte(`{"detail":[{"loc":["query","from"],"msg":"field required","type":"value_error.missing"}]}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"_test-http", "/test",
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
	if !strings.Contains(p.Error, "field required") {
		t.Errorf("expected error with validation details, got: %s", p.Error)
	}
}

func TestHTTPClient429RetriesThenSucceeds(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.Header().Set("X-Credits-Request", "5")
		w.Header().Set("X-Credits-Remaining", "995")
		w.WriteHeader(200)
		w.Write([]byte(`{"items":["ok"]}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"_test-http", "/test",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 after retry, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Verify successful response is returned, no partial output from failed attempt.
	parsed := parseEnvelope(t, result.Stdout)
	var data struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("failed to parse data: %v", err)
	}
	if len(data.Items) != 1 || data.Items[0] != "ok" {
		t.Errorf("expected successful response data, got %v", data.Items)
	}
}

func TestHTTPClient429NoRetryExitsCode3(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"--no-retry",
		"_test-http", "/test",
	)

	if result.ExitCode != 3 {
		t.Fatalf("expected exit 3, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 3 {
		t.Errorf("expected error code 3, got %d", p.Code)
	}
	if p.ErrorType != "rate_limited" {
		t.Errorf("expected error_type %q, got %q", "rate_limited", p.ErrorType)
	}
}

func TestHTTPClient5xxExitsCode5(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal server error"}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"_test-http", "/test",
	)

	if result.ExitCode != 5 {
		t.Fatalf("expected exit 5, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 5 {
		t.Errorf("expected error code 5, got %d", p.Code)
	}
	if p.ErrorType != "server_error" {
		t.Errorf("expected error_type %q, got %q", "server_error", p.ErrorType)
	}
	if p.Error != "Server error" {
		t.Errorf("expected error message %q, got %q", "Server error", p.Error)
	}
}

func TestHTTPClientNetworkErrorExitsCode5(t *testing.T) {
	// Point at a URL that will refuse connections.
	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", "http://127.0.0.1:1",
		"--no-retry",
		"--timeout", "2s",
		"_test-http", "/test",
	)

	if result.ExitCode != 5 {
		t.Fatalf("expected exit 5, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 5 {
		t.Errorf("expected error code 5, got %d", p.Code)
	}
	if p.ErrorType != "network_error" {
		t.Errorf("expected error_type %q, got %q", "network_error", p.ErrorType)
	}
}

// --- Boundary conditions ---

func TestHTTPClient429RetriesExhaustExitsCode3(t *testing.T) {
	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(429)
		w.Write([]byte(`{"error":"rate limited"}`))
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"_test-http", "/test",
	)

	if result.ExitCode != 3 {
		t.Fatalf("expected exit 3, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 3 {
		t.Errorf("expected error code 3, got %d", p.Code)
	}
	if !strings.Contains(p.Error, "Rate limited after 3 retries") {
		t.Errorf("expected message about exhausted retries, got: %s", p.Error)
	}
}

func TestHTTPClientTimeoutExitsCode5(t *testing.T) {
	done := make(chan struct{})
	defer close(done)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the test completes or 30s elapses, whichever comes first.
		// The CLI client should time out after 1s.
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"--timeout", "1s",
		"--no-retry",
		"_test-http", "/test",
	)

	if result.ExitCode != 5 {
		t.Fatalf("expected exit 5, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 5 {
		t.Errorf("expected error code 5, got %d", p.Code)
	}
	if p.ErrorType != "network_error" {
		t.Errorf("expected error_type %q, got %q", "network_error", p.ErrorType)
	}
}
