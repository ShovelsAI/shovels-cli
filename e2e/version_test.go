//go:build e2e

package e2e

import (
	"encoding/json"
	"testing"
)

func TestVersionOutputIsValidJSON(t *testing.T) {
	requireAPIKey(t)
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

	if envelope.Meta == nil {
		t.Error("meta field is missing")
	}
}

func TestVersionExitCodeZero(t *testing.T) {
	requireAPIKey(t)
	result := runCLI(t, "version")

	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
}
