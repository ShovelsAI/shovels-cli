package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestGlobalFlagsRegistered(t *testing.T) {
	expected := []string{"limit", "max-records", "base-url", "no-retry", "timeout"}
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

func TestExitErrorCode(t *testing.T) {
	err := &exitError{code: 2}
	if err.code != 2 {
		t.Errorf("expected code 2, got %d", err.code)
	}
	if err.Error() != "" {
		t.Errorf("expected empty error string, got %q", err.Error())
	}
}
