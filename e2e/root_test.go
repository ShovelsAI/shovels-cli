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
	requiredFlags := []string{"--limit", "--max-records", "--base-url", "--no-retry", "--timeout", "--dry-run"}
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

// --- Unhonored global flags: happy paths ---

func TestUnhonoredGlobalFlagRejectedNamingFlagAndCommand(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env, "--limit", "3", "config", "show")

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
	if p.Error != `--limit is not supported by "config show"` {
		t.Errorf("expected the error to name the flag and the command, got: %s", p.Error)
	}
}

func TestUnhonoredGlobalFlagLeavesStdoutEmpty(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env, "--limit", "3", "config", "show")

	if strings.TrimSpace(result.Stdout) != "" {
		t.Errorf("stdout must stay parseable-or-empty on a flag error, got: %s", result.Stdout)
	}
}

// --- Unhonored global flags: edge cases ---

// Explicitly passing an ignored flag is the error, so the value is irrelevant:
// rejection keys off whether the flag was typed, not off what it holds.
func TestUnhonoredGlobalFlagRejectedAtItsDefaultValue(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env, "--limit", "50", "config", "show")

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1 for the default value typed explicitly, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
}

// --- Unhonored global flags: error conditions ---

// --dry-run is registered last and sorts first, and is typed here ahead of
// --limit. Only root's registration order produces the expected message: the
// command line yields the typed order, and walking the flag set yields pflag's
// lexical one.
func TestSeveralUnhonoredGlobalFlagsNamedInRegistrationOrder(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env, "--dry-run", "--limit", "3", "version")

	p := parseStderrError(t, result.Stderr)
	if p.Error != `--limit, --dry-run are not supported by "version"` {
		t.Errorf("expected the flags named in registration order, got: %s", p.Error)
	}
}

func TestUnhonoredGlobalFlagPreemptsTheAuthError(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	result := runCLIWithEnv(t, env, "--limit", "3", "usage")

	if result.ExitCode != 1 {
		t.Fatalf("expected the flag error's exit 1 rather than auth's exit 2, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.ErrorType != "validation_error" {
		t.Errorf("expected error_type %q, got %q", "validation_error", p.ErrorType)
	}
}

// --- Unhonored global flags: boundary conditions ---

// A parent runs no code of its own, so cobra never reaches PersistentPreRunE
// and the contract cannot be enforced there. Help at exit 0 is what it keeps.
func TestUnhonoredGlobalFlagOnNonRunnableParentPrintsHelp(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env, "--limit", "3", "contractors")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "Available Commands:") {
		t.Errorf("expected help on stdout, got: %s", result.Stdout)
	}
}

// completion is cobra's, generated during Execute rather than declared in this
// tree. Rejection follows runnability, so it lands on cobra's leaves too.
func TestUnhonoredGlobalFlagRejectedOnGeneratedCompletionLeaf(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env, "--limit", "3", "completion", "bash")

	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	p := parseStderrError(t, result.Stderr)
	if p.Error != `--limit is not supported by "completion bash"` {
		t.Errorf("expected the error to name the completion leaf, got: %s", p.Error)
	}
}

// The help flag short-circuits ahead of PersistentPreRunE, so a command asked
// for help answers with it rather than with a flag error.
func TestUnhonoredGlobalFlagWithHelpFlagStillPrintsHelp(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env, "--limit", "3", "config", "show", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "Usage:") {
		t.Errorf("expected help on stdout, got: %s", result.Stdout)
	}
}
