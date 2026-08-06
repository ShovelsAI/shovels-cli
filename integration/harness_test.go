//go:build integration

// Package integration holds the live contract tests: a thin smoke suite that
// talks to the real Shovels API.
//
// It exists because nothing else in this repo does. The e2e suite is entirely
// httptest stubs, so it verifies "the CLI sends what the CLI thinks it should",
// never "what the API accepts". That gap is why ENG-4040 shipped, and why an
// API deploy tightening /properties/search broke CLI v0.8.0 with no test
// noticing — the shipped binary was sending the comma form its own --help
// documented, and the endpoint had started rejecting it.
//
// This suite is deliberately small. It is a contract smoke, not a second e2e
// suite: every case here must be one that a stubbed test cannot express,
// because it depends on what the live API does. Anything that can be pinned
// locally belongs in cmd/ or e2e/, which cost nothing to run.
//
// Cost, measured: a search costs 1 credit, a 422 costs 0, and contractor metrics,
// coverage, and version are credit-exempt. One pass is 9 authenticated requests
// (5 searches, the sentinel, contractor metrics, coverage, version) and 5 billable
// credits. Running against two binaries doubles both, but not the OpenAPI fetch,
// which is unauthenticated and runs once in its own job.
package integration

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// binaryPath is the shovels binary under test.
var binaryPath string

// TestMain resolves the binary once. SHOVELS_TEST_BINARY lets the same
// assertions run against a downloaded release rather than a build of HEAD,
// which is the point: the API deploy that broke v0.8.0 would not have been
// caught by a suite that only ever tests HEAD. A suite built from source stays
// green while the version users actually have is broken.
func TestMain(m *testing.M) {
	if p := os.Getenv("SHOVELS_TEST_BINARY"); p != "" {
		abs, err := filepath.Abs(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "SHOVELS_TEST_BINARY %q is not resolvable: %v\n", p, err)
			os.Exit(1)
		}
		if _, err := os.Stat(abs); err != nil {
			fmt.Fprintf(os.Stderr, "SHOVELS_TEST_BINARY %q does not exist: %v\n", abs, err)
			os.Exit(1)
		}
		binaryPath = abs
		code := m.Run()
		os.Exit(code)
	}

	tmpDir, err := os.MkdirTemp("", "shovels-integration-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	binaryPath = filepath.Join(tmpDir, "shovels")
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

// moduleRoot returns the module root; integration tests live one level below.
func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("cannot determine working directory: %v", err))
	}
	return filepath.Dir(dir)
}

// requireAPIKey fails rather than skips. Skipping is defensible in e2e, which
// is entirely stubbed and needs no key at all. Here a missing key means the
// suite verified nothing, and this layer is only ever run deliberately, so a
// silent skip would report success for zero coverage.
//
// The env var specifically: the subprocess runs with a scratch HOME, so a key
// stored via `shovels config set api-key` is deliberately not visible to it.
func requireAPIKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("SHOVELS_API_KEY")
	if key == "" {
		t.Fatal("the SHOVELS_API_KEY environment variable is required: this suite talks " +
			"to the live API, and running it without a key would pass while testing " +
			"nothing. A key stored via `shovels config set api-key` will not do — the " +
			"subprocess runs with a scratch HOME so the config file is out of scope.")
	}
	return key
}

// CLIResult holds captured output from a CLI invocation.
type CLIResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// runCLI executes the binary under test against the live API.
//
// The subprocess environment is set explicitly rather than inherited:
//
//   - CI=1 disables the background self-updater (see autoupdateDisabled in
//     cmd/root.go). A build of HEAD is protected by buildVersion == "dev", but
//     a downloaded release is not — without this it could replace itself
//     mid-run, which would silently invalidate the very thing being tested.
//   - HOME and XDG_CONFIG_HOME point at a scratch dir so a developer's
//     ~/.config/shovels/config.yaml cannot change the result.
//   - SHOVELS_API_KEY is passed through deliberately, so the only credential in
//     play is the one this suite was given.
//
// Relying on the harness to set CI is not enough: this suite is meant to run
// from the API deploy pipeline too, where that assumption may not hold.
func runCLI(t *testing.T, args ...string) CLIResult {
	t.Helper()

	key := requireAPIKey(t)
	home := t.TempDir()

	cmd := exec.Command(binaryPath, args...)
	cmd.Env = []string{
		"CI=1",
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"SHOVELS_API_KEY=" + key,
		"PATH=" + os.Getenv("PATH"),
	}
	// Proxy settings are the one thing worth inheriting. An explicit env that
	// omits them makes this suite unrunnable behind a corporate proxy, which
	// would be blamed on the API rather than on the harness.
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "no_proxy"} {
		if v := os.Getenv(name); v != "" {
			cmd.Env = append(cmd.Env, name+"="+v)
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

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
