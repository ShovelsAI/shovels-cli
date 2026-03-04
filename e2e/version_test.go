//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionOutputIsValidJSON(t *testing.T) {
	result := runCLI(t, "version")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	var envelope struct {
		Data struct {
			Version string `json:"version"`
			Commit  string `json:"commit"`
			Date    string `json:"date"`
		} `json:"data"`
		Meta map[string]any `json:"meta"`
	}

	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}

	if envelope.Data.Version == "" {
		t.Error("data.version is empty")
	}

	if envelope.Data.Commit == "" {
		t.Error("data.commit is empty")
	}

	if envelope.Data.Date == "" {
		t.Error("data.date is empty")
	}

	if envelope.Meta == nil {
		t.Error("meta field is missing")
	}
}

func TestVersionExitCodeZero(t *testing.T) {
	result := runCLI(t, "version")

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
}

func TestVersionWithAPIKeyIncludesDataReleaseDate(t *testing.T) {
	requireAPIKey(t)

	result := runCLI(t, "version")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}

	releaseDate, ok := envelope.Data["data_release_date"]
	if !ok {
		t.Fatal("data_release_date field is missing from version output")
	}

	// With a valid API key, data_release_date should be a non-empty string.
	dateStr, isString := releaseDate.(string)
	if !isString || dateStr == "" {
		t.Errorf("expected data_release_date to be a non-empty string, got %v", releaseDate)
	}
}

func TestVersionWithoutAPIKeyHasNullDataReleaseDate(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)
	result := runCLIWithEnv(t, env, "version")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if result.Stderr != "" {
		t.Errorf("expected empty stderr for silent degradation, got: %s", result.Stderr)
	}

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}

	releaseDate, ok := envelope.Data["data_release_date"]
	if !ok {
		t.Fatal("data_release_date field is missing from version output")
	}

	if releaseDate != nil {
		t.Errorf("expected data_release_date to be null without API key, got %v", releaseDate)
	}

	for _, key := range []string{"version", "commit", "date"} {
		if _, exists := envelope.Data[key]; !exists {
			t.Errorf("build info field %q is missing", key)
		}
	}
}

func TestVersionWithUnreachableAPIHasNullDataReleaseDate(t *testing.T) {
	tmpDir := t.TempDir()
	result := runCLIWithEnv(t,
		[]string{
			"SHOVELS_API_KEY=test-key",
			"XDG_CONFIG_HOME=" + tmpDir,
		},
		"version", "--base-url", "http://127.0.0.1:1",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if result.Stderr != "" {
		t.Errorf("expected empty stderr for silent degradation, got: %s", result.Stderr)
	}

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}

	releaseDate := envelope.Data["data_release_date"]
	if releaseDate != nil {
		t.Errorf("expected data_release_date to be null with unreachable API, got %v", releaseDate)
	}

	for _, key := range []string{"version", "commit", "date"} {
		if _, exists := envelope.Data[key]; !exists {
			t.Errorf("build info field %q is missing", key)
		}
	}
}

func TestVersionWithAPI500HasNullDataReleaseDate(t *testing.T) {
	// Spin up a test server that returns 500 for /meta/release.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"internal server error"}`)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	result := runCLIWithEnv(t,
		[]string{
			"SHOVELS_API_KEY=test-key",
			"XDG_CONFIG_HOME=" + tmpDir,
		},
		"version", "--base-url", srv.URL,
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if result.Stderr != "" {
		t.Errorf("expected empty stderr for silent degradation on API 500, got: %s", result.Stderr)
	}

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}

	releaseDate := envelope.Data["data_release_date"]
	if releaseDate != nil {
		t.Errorf("expected data_release_date to be null on API 500, got %v", releaseDate)
	}

	for _, key := range []string{"version", "commit", "date"} {
		if _, exists := envelope.Data[key]; !exists {
			t.Errorf("build info field %q is missing", key)
		}
	}
}

func TestVersionWithMalformedConfigAndEnvKeyFetchesReleaseDate(t *testing.T) {
	// Spin up a test server that returns a valid release date for /meta/release.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"released_at":"2026-02-28"}`)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "shovels")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("': bad yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := runCLIWithEnv(t,
		[]string{
			"XDG_CONFIG_HOME=" + tmpDir,
			"SHOVELS_API_KEY=sk-test-key",
		},
		"version", "--base-url", srv.URL,
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if result.Stderr != "" {
		t.Errorf("expected empty stderr, got: %s", result.Stderr)
	}

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}

	releaseDate, ok := envelope.Data["data_release_date"]
	if !ok {
		t.Fatal("data_release_date field is missing from version output")
	}

	// With a valid env API key, the fallback config must still fetch data_release_date.
	dateStr, isString := releaseDate.(string)
	if !isString || dateStr == "" {
		t.Errorf("expected data_release_date to be a non-empty string, got %v", releaseDate)
	}
	if dateStr != "2026-02-28" {
		t.Errorf("expected data_release_date %q, got %q", "2026-02-28", dateStr)
	}
}

func TestVersionWithMalformedConfigExitsZero(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "shovels")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("': bad yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := runCLIWithEnv(t,
		[]string{
			"XDG_CONFIG_HOME=" + tmpDir,
			"SHOVELS_API_KEY=",
		},
		"version",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0 with malformed config, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if result.Stderr != "" {
		t.Errorf("expected empty stderr for silent degradation on malformed config, got: %s", result.Stderr)
	}

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}

	releaseDate := envelope.Data["data_release_date"]
	if releaseDate != nil {
		t.Errorf("expected data_release_date to be null with malformed config, got %v", releaseDate)
	}

	for _, key := range []string{"version", "commit", "date"} {
		if _, exists := envelope.Data[key]; !exists {
			t.Errorf("build info field %q is missing", key)
		}
	}
}

func TestVersionOutputHasDataReleaseDateField(t *testing.T) {
	result := runCLI(t, "version")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}

	if _, ok := envelope.Data["data_release_date"]; !ok {
		t.Error("data_release_date field must be present in version output")
	}
}

func TestVersionHelpMentionsDataReleaseDate(t *testing.T) {
	result := runCLI(t, "version", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	if !strings.Contains(result.Stdout, "data_release_date") {
		t.Error("version --help should mention data_release_date")
	}

	if !strings.Contains(strings.ToLower(result.Stdout), "data") && !strings.Contains(strings.ToLower(result.Stdout), "freshness") {
		t.Error("version --help should mention data freshness concept")
	}
}
