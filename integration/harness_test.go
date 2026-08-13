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
// Most assertions here hold against any binary: what the API accepts and
// returns does not depend on which version asked, and that is the suite's
// reason to exist.
//
// The exception is an assertion about output the CLI only began producing in a
// known version. It is written beside the code that satisfies it, so it is
// false for every release published earlier and true from that version on.
// Those call requireVersionAtLeast, naming the version. Output shape that
// predates the oldest release this lane runs — the data/meta envelope itself,
// the version payload, stdout staying empty on error — needs no gate and is
// asserted everywhere.
//
// The distinction is not cosmetic: a released-lane failure is reported as "the
// current release is already broken for users", so an ungated assertion that
// cannot hold there raises that alarm every night until the next release, in
// the same words a real contract break would use.
//
// Cost, measured: a search costs 1 credit, a 422 costs 0, and contractor metrics,
// coverage, and version are credit-exempt. One pass is 10 authenticated requests
// (5 searches, the sentinel, contractor metrics twice, coverage, version) and 5
// billable credits; a binary predating an assertion's version skips it and makes
// one fewer. Resolving a supplied binary's version reaches no API at all: with
// no key in its scratch HOME, `version` reports the build stamp alone. The
// OpenAPI fetch is unauthenticated and runs once in its own job rather than per
// binary.
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// binaryPath is the shovels binary under test.
var binaryPath string

// binaryVersion is what the binary under test reports for itself: a semver for
// a published release, "dev" for a build of this tree.
var binaryVersion string

// requireVersionAtLeast skips an assertion about output the CLI began producing
// in a known version, when the binary under test predates it.
//
// Such an assertion is written beside the code that satisfies it, so it is false
// for every release published earlier and true from that version on. Running it
// against an older download reports the release broken on the day the assertion
// merges, which is how a healthy v0.8.4 spent three nights filing contract-drift
// issues against itself.
//
// The version is the gate, not the origin of the binary. Skipping every download
// would retire the assertion from the released lane permanently, including for
// the releases that do satisfy it — trading a false alarm for a blind spot, in
// the one lane that speaks for what users are running.
//
// It skips rather than passing quietly: the lane must say which assertions it
// did not make, or a green run reads as one that checked everything.
func requireVersionAtLeast(t *testing.T, minimum string) {
	t.Helper()
	if binaryVersion == devVersion {
		return
	}
	if compareVersions(binaryVersion, minimum) < 0 {
		t.Skipf("asserts output introduced in v%s; the binary under test reports v%s", minimum, binaryVersion)
	}
}

// devVersion is what a binary built from source reports, per cmd/root.go's
// buildVersion default. It always satisfies a minimum: a build of this tree is
// the code the assertion was written beside.
const devVersion = "dev"

// compareVersions orders two semver versions, returning -1, 0 or 1.
//
// A prerelease sorts BEFORE the version it qualifies: 0.9.0-rc.1 precedes
// 0.9.0, so a release candidate is not credited with output the release it
// precedes introduced. Splitting on "." alone gets this backwards — "0-rc" is
// one more component than "0", which reads as the larger version.
func compareVersions(a, b string) int {
	aNumeric, aPre := splitPrerelease(a)
	bNumeric, bPre := splitPrerelease(b)

	aParts := strings.Split(aNumeric, ".")
	bParts := strings.Split(bNumeric, ".")
	for i := 0; i < len(aParts) || i < len(bParts); i++ {
		an, bn := versionPart(aParts, i), versionPart(bParts, i)
		if an != bn {
			if an < bn {
				return -1
			}
			return 1
		}
	}

	switch {
	case aPre && !bPre:
		return -1
	case !aPre && bPre:
		return 1
	}
	return 0
}

// splitPrerelease separates the numeric core of a version from any prerelease
// or build suffix, reporting whether one was present.
func splitPrerelease(v string) (numeric string, prerelease bool) {
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		return v[:i], true
	}
	return v, false
}

// versionPart returns the numeric value of one dotted component, 0 when the
// component is absent or non-numeric.
func versionPart(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	n, err := strconv.Atoi(parts[i])
	if err != nil {
		return 0
	}
	return n
}

// resolveBinaryVersion asks the binary what it is. The suite gates on the
// version rather than on how the path arrived, so a build of this tree handed
// over by SHOVELS_TEST_BINARY is asserted against rather than skipped with a
// reason that would not be true of it.
//
// It runs with an empty scratch HOME and CI=1 for the same reasons runCLI does,
// and reaches no API: with no key resolvable, `version` reports the build stamp
// alone. A shared HOME would not do — an ambient config file there would give it
// a key and turn this into a live call.
func resolveBinaryVersion() (string, error) {
	home, err := os.MkdirTemp("", "shovels-version-home-*")
	if err != nil {
		return "", fmt.Errorf("creating a scratch HOME: %w", err)
	}
	defer os.RemoveAll(home)

	cmd := exec.Command(binaryPath, "version")
	cmd.Env = []string{"CI=1", "HOME=" + home, "PATH=" + os.Getenv("PATH")}
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("running `%s version`: %w", binaryPath, err)
	}

	var envelope struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &envelope); err != nil {
		return "", fmt.Errorf("parsing `%s version` output %q: %w", binaryPath, out, err)
	}
	if envelope.Data.Version == "" {
		return "", fmt.Errorf("`%s version` reported no version: %s", binaryPath, out)
	}
	return envelope.Data.Version, nil
}

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
		if binaryVersion, err = resolveBinaryVersion(); err != nil {
			fmt.Fprintf(os.Stderr, "cannot determine the version of SHOVELS_TEST_BINARY %q: %v\n", abs, err)
			os.Exit(1)
		}
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
	binaryVersion = devVersion

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
