package cmd

import (
	"testing"
)

func TestGlobalFlagsRegistered(t *testing.T) {
	expected := []string{"api-key", "limit", "max-records", "base-url", "no-retry", "timeout"}
	flags := rootCmd.PersistentFlags()

	for _, name := range expected {
		if flags.Lookup(name) == nil {
			t.Errorf("expected persistent flag %q to be registered on root command", name)
		}
	}
}

func TestAPIKeyAccessibleFromSubcommand(t *testing.T) {
	// Persistent flags on root are inherited by all subcommands.
	// Verify that a subcommand (version) can look up --api-key.
	f := versionCmd.InheritedFlags()
	if f.Lookup("api-key") == nil {
		t.Error("--api-key persistent flag not accessible from version subcommand")
	}
}

func TestAPIKeyDefaultEmpty(t *testing.T) {
	val, err := rootCmd.PersistentFlags().GetString("api-key")
	if err != nil {
		t.Fatalf("unexpected error getting api-key flag: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty default for --api-key, got %q", val)
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
