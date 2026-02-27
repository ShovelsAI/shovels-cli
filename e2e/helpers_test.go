//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// binaryPath holds the absolute path to the compiled shovels binary,
// built once in TestMain and reused by all e2e tests.
var binaryPath string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "shovels-e2e-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	binaryPath = filepath.Join(tmpDir, "shovels")

	// Build the binary from the project root (one directory up from e2e/).
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = moduleRoot()
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "binary build failed: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(tmpDir)
	os.Exit(code)
}

// moduleRoot returns the absolute path to the Go module root.
func moduleRoot() string {
	// e2e tests live in <root>/e2e/, so the module root is one level up.
	dir, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("cannot determine working directory: %v", err))
	}
	return filepath.Dir(dir)
}

// CLIResult holds captured output from a CLI invocation.
type CLIResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// runCLI executes the shovels binary with the given arguments and returns
// stdout, stderr, and exit code separately. Environment variables from the
// current process are inherited; additional env vars can be prepended.
func runCLI(t *testing.T, args ...string) CLIResult {
	t.Helper()
	return runCLIWithEnv(t, nil, args...)
}

// runCLIWithEnv executes the shovels binary with extra environment variables.
// Each entry in env should be "KEY=VALUE". These are appended to os.Environ().
func runCLIWithEnv(t *testing.T, env []string, args ...string) CLIResult {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}

	err := cmd.Run()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("failed to run CLI: %v", err)
	}

	return CLIResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

// requireAPIKey skips the test if SHOVELS_API_KEY is not set.
func requireAPIKey(t *testing.T) {
	t.Helper()
	if os.Getenv("SHOVELS_API_KEY") == "" {
		t.Skip("SHOVELS_API_KEY not set")
	}
}
