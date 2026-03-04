package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestMetaFetchTimeoutIs2Seconds(t *testing.T) {
	if metaFetchTimeout != 2*time.Second {
		t.Errorf("expected metaFetchTimeout to be 2s, got %v", metaFetchTimeout)
	}
}

func TestVersionExitCodeAlwaysZero(t *testing.T) {
	// Simulate version command execution and verify it uses Run (not RunE),
	// meaning it never returns an error and always exits 0.
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	versionCmd.Run(cmd, nil)

	// If we got here without panic, the command succeeded.
	// Verify output is valid JSON.
	var envelope map[string]any
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("version output is not valid JSON: %v", err)
	}
}

func TestVersionCommandUsesRunNotRunE(t *testing.T) {
	// Version uses Run (infallible) not RunE (fallible), guaranteeing exit 0.
	if versionCmd.Run == nil {
		t.Error("version command should use Run (infallible), not RunE")
	}
	if versionCmd.RunE != nil {
		t.Error("version command should not use RunE — version must never fail")
	}
}

func TestFetchDataReleaseDateSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/meta/release" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(200)
		w.Write([]byte(`{"released_at": "2026-02-28"}`))
	}))
	defer srv.Close()

	result := fetchDataReleaseDate(context.Background(), "test-key", srv.URL)
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if *result != "2026-02-28" {
		t.Errorf("expected 2026-02-28, got %s", *result)
	}
}

func TestFetchDataReleaseDateServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer srv.Close()

	result := fetchDataReleaseDate(context.Background(), "test-key", srv.URL)
	if result != nil {
		t.Errorf("expected nil on server error, got %s", *result)
	}
}

func TestFetchDataReleaseDateMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	result := fetchDataReleaseDate(context.Background(), "test-key", srv.URL)
	if result != nil {
		t.Errorf("expected nil on malformed JSON, got %s", *result)
	}
}

func TestFetchDataReleaseDateEmptyReleasedAt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"released_at": ""}`))
	}))
	defer srv.Close()

	result := fetchDataReleaseDate(context.Background(), "test-key", srv.URL)
	if result != nil {
		t.Errorf("expected nil on empty released_at, got %s", *result)
	}
}

func TestFetchDataReleaseDateNetworkError(t *testing.T) {
	// Use a URL that will fail to connect.
	result := fetchDataReleaseDate(context.Background(), "test-key", "http://127.0.0.1:1")
	if result != nil {
		t.Errorf("expected nil on network error, got %s", *result)
	}
}

func TestVersionOutputIncludesDataReleaseDate(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)

	versionCmd.Run(cmd, nil)

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	// data_release_date key must exist (value is null without API key).
	if _, ok := envelope.Data["data_release_date"]; !ok {
		t.Error("version output must include data_release_date field")
	}
}
