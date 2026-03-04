package cmd

import (
	"testing"
	"time"

	"github.com/shovels-ai/shovels-cli/internal/config"
	"github.com/shovels-ai/shovels-cli/internal/update"
	"github.com/spf13/cobra"
)

func TestGlobalFlagsRegistered(t *testing.T) {
	expected := []string{"limit", "max-records", "base-url", "no-retry", "timeout", "dry-run"}
	flags := rootCmd.PersistentFlags()

	for _, name := range expected {
		if flags.Lookup(name) == nil {
			t.Errorf("expected persistent flag %q to be registered on root command", name)
		}
	}
}

func TestLimitDefaultIs50(t *testing.T) {
	val, err := rootCmd.PersistentFlags().GetString("limit")
	if err != nil {
		t.Fatalf("unexpected error getting limit flag: %v", err)
	}
	if val != "50" {
		t.Errorf("expected default limit %q, got %q", "50", val)
	}
}

func TestMaxRecordsDefaultIs10000(t *testing.T) {
	val, err := rootCmd.PersistentFlags().GetInt("max-records")
	if err != nil {
		t.Fatalf("unexpected error getting max-records flag: %v", err)
	}
	if val != 10000 {
		t.Errorf("expected default max-records %d, got %d", 10000, val)
	}
}

func TestBaseURLDefault(t *testing.T) {
	val, err := rootCmd.PersistentFlags().GetString("base-url")
	if err != nil {
		t.Fatalf("unexpected error getting base-url flag: %v", err)
	}
	if val != "https://api.shovels.ai/v2" {
		t.Errorf("expected default base-url %q, got %q", "https://api.shovels.ai/v2", val)
	}
}

func TestTimeoutDefault(t *testing.T) {
	val, err := rootCmd.PersistentFlags().GetString("timeout")
	if err != nil {
		t.Fatalf("unexpected error getting timeout flag: %v", err)
	}
	if val != "30s" {
		t.Errorf("expected default timeout %q, got %q", "30s", val)
	}
}

func TestNoRetryDefaultFalse(t *testing.T) {
	val, err := rootCmd.PersistentFlags().GetBool("no-retry")
	if err != nil {
		t.Fatalf("unexpected error getting no-retry flag: %v", err)
	}
	if val {
		t.Error("expected --no-retry default to be false")
	}
}

func TestRequiresAuthWithAnnotation(t *testing.T) {
	cmd := &cobra.Command{
		Annotations: map[string]string{AnnotationRequiresAuth: "true"},
	}
	if !requiresAuth(cmd) {
		t.Error("requiresAuth should return true for annotated command")
	}
}

func TestRequiresAuthWithoutAnnotation(t *testing.T) {
	cmd := &cobra.Command{}
	if requiresAuth(cmd) {
		t.Error("requiresAuth should return false for unannotated command")
	}
}

func TestRequiresAuthInheritsFromParent(t *testing.T) {
	parent := &cobra.Command{
		Annotations: map[string]string{AnnotationRequiresAuth: "true"},
	}
	child := &cobra.Command{}
	parent.AddCommand(child)

	if !requiresAuth(child) {
		t.Error("requiresAuth should return true when parent is annotated")
	}
}

func TestVersionDoesNotRequireAuth(t *testing.T) {
	if requiresAuth(versionCmd) {
		t.Error("version command should not require auth")
	}
}

func TestConfigDoesNotRequireAuth(t *testing.T) {
	if requiresAuth(configCmd) {
		t.Error("config command should not require auth")
	}
}

func TestDryRunDefaultFalse(t *testing.T) {
	val, err := rootCmd.PersistentFlags().GetBool("dry-run")
	if err != nil {
		t.Fatalf("unexpected error getting dry-run flag: %v", err)
	}
	if val {
		t.Error("expected --dry-run default to be false")
	}
}

func TestExitErrorCode(t *testing.T) {
	err := &exitError{code: 2}
	if err.code != 2 {
		t.Errorf("expected code 2, got %d", err.code)
	}
	if err.Error() != "" {
		t.Errorf("expected empty error string, got %q", err.Error())
	}
}

func TestAutoupdateDisabled_DevBuild(t *testing.T) {
	old := buildVersion
	buildVersion = "dev"
	defer func() { buildVersion = old }()

	cfg := config.Config{}
	if !autoupdateDisabled(cfg) {
		t.Error("expected autoupdate to be disabled for dev build")
	}
}

func TestAutoupdateDisabled_CIEnv(t *testing.T) {
	old := buildVersion
	buildVersion = "0.3.0"
	defer func() { buildVersion = old }()

	t.Setenv("CI", "true")

	cfg := config.Config{}
	if !autoupdateDisabled(cfg) {
		t.Error("expected autoupdate to be disabled when CI env is set")
	}
}

func TestAutoupdateDisabled_ConfigFalse(t *testing.T) {
	old := buildVersion
	buildVersion = "0.3.0"
	defer func() { buildVersion = old }()

	t.Setenv("CI", "")

	v := false
	cfg := config.Config{Autoupdate: &v}
	if !autoupdateDisabled(cfg) {
		t.Error("expected autoupdate to be disabled when config is false")
	}
}

func TestAutoupdateEnabled_DefaultConfig(t *testing.T) {
	old := buildVersion
	buildVersion = "0.3.0"
	defer func() { buildVersion = old }()

	t.Setenv("CI", "")

	cfg := config.Config{}
	if autoupdateDisabled(cfg) {
		t.Error("expected autoupdate to be enabled with default config")
	}
}

// Behavior: GIVEN 10s total timeout WHEN goroutine still running at
// program exit THEN program waits up to remaining time from the 10s
// budget, then exits — even if the command itself returned an error.
// This verifies waitForUpdate runs on error paths via defer in Execute().
func TestWaitForUpdate_RunsOnCommandError(t *testing.T) {
	// Pre-populate the update channel as if maybeStartUpdate had run
	// before the command failed.
	ch := make(chan *update.Result, 1)
	ch <- &update.Result{Updated: true, OldVersion: "0.1.0", NewVersion: "0.2.0"}
	updateResultCh = ch
	updateCancel = func() {} // no-op cancel
	updateStartTime = time.Now()

	// Run a command that will fail (unknown subcommand). Cobra returns
	// an error without calling PersistentPostRunE.
	rootCmd.SetArgs([]string{"nonexistent-subcommand-xyz"})
	code := Execute()
	// Reset args so other tests are not affected.
	rootCmd.SetArgs(nil)

	if code == 0 {
		t.Error("expected non-zero exit code for unknown subcommand")
	}

	// The defer in Execute() should have drained the channel via
	// waitForUpdate(). If the channel still has a value, the wait
	// did not run.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected update result channel to be drained by waitForUpdate")
		}
	default:
		// Channel is empty — waitForUpdate consumed the result.
	}
}

func TestWaitForUpdate_NilChannel(t *testing.T) {
	// Ensure waitForUpdate doesn't panic when no goroutine was started.
	updateResultCh = nil
	updateCancel = nil
	waitForUpdate()
}
