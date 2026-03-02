//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// makeUsageHandler returns an HTTP handler that serves a usage response.
// The handler returns credit information in both the response body and headers.
func makeUsageHandler(creditsUsed, creditLimit int, setCreditHeaders bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if setCreditHeaders {
			w.Header().Set("X-Credits-Request", "0")
			w.Header().Set("X-Credits-Remaining", "9999")
		}

		resp := map[string]any{
			"credits_used": creditsUsed,
			"credit_limit": creditLimit,
		}
		json.NewEncoder(w).Encode(resp)
	})
}

// makeUnlimitedUsageHandler returns an HTTP handler that serves a usage
// response for an unlimited plan where credit_limit is null.
func makeUnlimitedUsageHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Credits-Request", "0")

		// Unlimited plan: credit_limit is null.
		w.Write([]byte(`{"credits_used":500,"credit_limit":null}`))
	})
}

// --- Happy paths ---

func TestUsageBasic(t *testing.T) {
	srv := httptest.NewServer(makeUsageHandler(150, 10000, true))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"usage",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// Verify data is an object (not an array).
	var data map[string]any
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data to be an object: %v\ndata: %s", err, string(parsed.Data))
	}

	cu := int(data["credits_used"].(float64))
	if cu != 150 {
		t.Errorf("expected data.credits_used=150, got %d", cu)
	}

	cl := int(data["credit_limit"].(float64))
	if cl != 10000 {
		t.Errorf("expected data.credit_limit=10000, got %d", cl)
	}

	// Verify meta has credits but no count/has_more.
	if _, ok := parsed.Meta["count"]; ok {
		t.Error("non-paginated response should not have count in meta")
	}
	if _, ok := parsed.Meta["has_more"]; ok {
		t.Error("non-paginated response should not have has_more in meta")
	}

	metaCU := int(parsed.Meta["credits_used"].(float64))
	if metaCU != 0 {
		t.Errorf("expected meta.credits_used=0, got %d", metaCU)
	}

	metaCR := int(parsed.Meta["credits_remaining"].(float64))
	if metaCR != 9999 {
		t.Errorf("expected meta.credits_remaining=9999, got %d", metaCR)
	}
}

// --- Edge cases ---

func TestUsageUnlimitedPlan(t *testing.T) {
	srv := httptest.NewServer(makeUnlimitedUsageHandler())
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"usage",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// Verify data has credit_limit as null.
	var data map[string]any
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data to be an object: %v", err)
	}

	if data["credit_limit"] != nil {
		t.Errorf("expected data.credit_limit to be null for unlimited plan, got %v", data["credit_limit"])
	}

	cu := int(data["credits_used"].(float64))
	if cu != 500 {
		t.Errorf("expected data.credits_used=500, got %d", cu)
	}
}

// --- Boundary conditions ---

func TestUsageNonPaginatedNoCountHasMore(t *testing.T) {
	srv := httptest.NewServer(makeUsageHandler(0, 5000, true))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--api-key", "sk-test",
		"--base-url", srv.URL,
		"usage",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// meta must NOT contain count or has_more for non-paginated responses.
	if _, ok := parsed.Meta["count"]; ok {
		t.Error("usage meta must not contain count")
	}
	if _, ok := parsed.Meta["has_more"]; ok {
		t.Error("usage meta must not contain has_more")
	}

	// meta should still have credits.
	if _, ok := parsed.Meta["credits_used"]; !ok {
		t.Error("expected credits_used in meta")
	}
	if _, ok := parsed.Meta["credits_remaining"]; !ok {
		t.Error("expected credits_remaining in meta")
	}
}

func TestUsageRequiresAuth(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env, "usage")

	if result.ExitCode != 2 {
		t.Fatalf("expected exit 2 with no API key, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "auth_error" {
		t.Errorf("expected error_type %q, got %q", "auth_error", p.ErrorType)
	}
}
