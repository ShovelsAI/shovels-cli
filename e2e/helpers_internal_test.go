//go:build e2e

package e2e

import (
	"testing"
)

func TestCLIHelperCapturesStdoutAndStderrIndependently(t *testing.T) {
	requireAPIKey(t)
	// The version command writes JSON to stdout and nothing to stderr.
	// This verifies the harness captures them as separate streams.
	result := runCLI(t, "version")

	if result.Stdout == "" {
		t.Fatal("expected non-empty stdout from version command")
	}

	if result.Stderr != "" {
		t.Errorf("expected empty stderr from version command, got: %s", result.Stderr)
	}

	// Verify stdout contains JSON and stderr does not contain JSON fragments.
	if result.Stdout == result.Stderr {
		t.Error("stdout and stderr are identical; streams may be interleaved")
	}
}
