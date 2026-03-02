//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

func TestHelpShowsCommandsAndGlobalFlags(t *testing.T) {
	result := runCLI(t, "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Verify available commands are listed.
	if !strings.Contains(result.Stdout, "version") {
		t.Error("--help output should list the version command")
	}

	// Verify global flags are present.
	requiredFlags := []string{"--limit", "--max-records", "--base-url", "--no-retry", "--timeout"}
	for _, flag := range requiredFlags {
		if !strings.Contains(result.Stdout, flag) {
			t.Errorf("--help output should contain global flag %q", flag)
		}
	}
}

func TestNoArgsShowsHelp(t *testing.T) {
	result := runCLI(t)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 with no args, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// No-args output should match --help output.
	helpResult := runCLI(t, "--help")
	if result.Stdout != helpResult.Stdout {
		t.Error("no-args output should match --help output")
	}
}

func TestUnknownCommandProducesJSONStderr(t *testing.T) {
	result := runCLI(t, "foobar")

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1 for unknown command, got %d", result.ExitCode)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if p.ErrorType != "client_error" {
		t.Errorf("expected error_type %q, got %q", "client_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "unknown command") {
		t.Errorf("expected error to mention 'unknown command', got: %s", p.Error)
	}
}

func TestUnknownFlagProducesJSONStderr(t *testing.T) {
	result := runCLI(t, "--unknown-flag")

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1 for unknown flag, got %d", result.ExitCode)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if p.ErrorType != "client_error" {
		t.Errorf("expected error_type %q, got %q", "client_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "unknown flag") {
		t.Errorf("expected error to mention 'unknown flag', got: %s", p.Error)
	}

	// Verify stdout is empty (no plain text leakage).
	if strings.TrimSpace(result.Stdout) != "" {
		t.Errorf("stdout should be empty for flag error, got: %s", result.Stdout)
	}
}
