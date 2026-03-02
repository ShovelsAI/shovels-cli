//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// TestRootHelpShowsDescriptionCommandsAndGlobalFlags verifies that
// `shovels --help` displays a one-line description, lists all resource
// commands, and shows global flags with their default values.
func TestRootHelpShowsDescriptionCommandsAndGlobalFlags(t *testing.T) {
	result := runCLI(t, "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	// Description must mention building permits and contractors.
	if !strings.Contains(out, "building permits") {
		t.Error("root --help should mention building permits")
	}
	if !strings.Contains(out, "contractors") {
		t.Error("root --help should mention contractors")
	}

	// All resource commands must be listed.
	commands := []string{"permits", "contractors", "addresses", "usage", "config", "version"}
	for _, cmd := range commands {
		if !strings.Contains(out, cmd) {
			t.Errorf("root --help should list the %q command", cmd)
		}
	}

	// Global flags must be present.
	globalFlags := []string{
		"--limit",
		"--max-records",
		"--base-url",
		"--no-retry",
		"--timeout",
	}
	for _, flag := range globalFlags {
		if !strings.Contains(out, flag) {
			t.Errorf("root --help should contain global flag %q", flag)
		}
	}

	// Default values must be visible in the help text.
	defaults := []struct {
		label string
		value string
	}{
		{"--limit default", "50"},
		{"--timeout default", "30s"},
		{"--base-url default", "https://api.shovels.ai/v2"},
		{"--max-records default", "10000"},
	}
	for _, d := range defaults {
		if !strings.Contains(out, d.value) {
			t.Errorf("root --help should show %s value %q", d.label, d.value)
		}
	}
}

// TestPermitsSearchHelpShowsGroupedFlagsAndExamples verifies that
// `shovels permits search --help` displays: a description, required flags
// marked "(required)", optional flags with types, example values, and
// flags grouped by category.
func TestPermitsSearchHelpShowsGroupedFlagsAndExamples(t *testing.T) {
	result := runCLI(t, "permits", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	// Description should be concrete.
	if !strings.Contains(out, "Search the Shovels building permits database") {
		t.Error("permits search --help should describe searching building permits")
	}

	// Required flags marked with "(required)".
	requiredFlags := []string{"--geo-id", "--permit-from", "--permit-to"}
	for _, flag := range requiredFlags {
		if !strings.Contains(out, flag) {
			t.Errorf("permits search --help should contain required flag %q", flag)
		}
	}
	if !strings.Contains(out, "(required)") {
		t.Error("permits search --help should mark required flags with (required)")
	}

	// Flag groups must be present as section headers.
	groups := []string{"Required Flags", "Permit Filters", "Property Filters", "Contractor Filters"}
	for _, group := range groups {
		if !strings.Contains(out, group) {
			t.Errorf("permits search --help should contain flag group %q", group)
		}
	}

	// Example values should be present.
	if !strings.Contains(out, "ZIP_90210") {
		t.Error("permits search --help should contain example value ZIP_90210")
	}
	if !strings.Contains(out, "YYYY-MM-DD") {
		t.Error("permits search --help should contain date format hint YYYY-MM-DD")
	}

	// All optional flags from shared search flags + permits-specific flags.
	optionalFlags := []string{
		// Permit filters
		"--tags",
		"--query",
		"--status",
		"--min-approval-duration",
		"--min-construction-duration",
		"--min-inspection-pr",
		"--min-job-value",
		"--min-fees",
		// Property filters
		"--property-type",
		"--property-min-market-value",
		"--property-min-building-area",
		"--property-min-lot-size",
		"--property-min-story-count",
		"--property-min-unit-count",
		// Contractor filters
		"--contractor-classification",
		"--contractor-name",
		"--contractor-website",
		"--contractor-min-total-job-value",
		"--contractor-min-total-permits-count",
		"--contractor-min-inspection-pr",
		"--contractor-license",
		// Permits-specific
		"--has-contractor",
	}
	for _, flag := range optionalFlags {
		if !strings.Contains(out, flag) {
			t.Errorf("permits search --help should contain optional flag %q", flag)
		}
	}

	// Type hints should be present for typed flags.
	typeHints := []string{"string", "int", "strings"}
	foundTypeHints := 0
	for _, hint := range typeHints {
		if strings.Contains(out, hint) {
			foundTypeHints++
		}
	}
	if foundTypeHints == 0 {
		t.Error("permits search --help should display type hints for flags")
	}

	// Global flags section should appear (inherited).
	if !strings.Contains(out, "Global Flags") {
		t.Error("permits search --help should contain a Global Flags section")
	}
}

// TestPermitsHelpListsSubcommands verifies that `shovels permits --help`
// lists the available subcommands: search and get.
func TestPermitsHelpListsSubcommands(t *testing.T) {
	result := runCLI(t, "permits", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	if !strings.Contains(out, "search") {
		t.Error("permits --help should list the search subcommand")
	}
	if !strings.Contains(out, "get") {
		t.Error("permits --help should list the get subcommand")
	}
}

// TestHelpOutputIsPlainText verifies that --help output is plain text and
// not JSON. Help text is the sole exception to the JSON-only output rule.
func TestHelpOutputIsPlainText(t *testing.T) {
	commands := [][]string{
		{"--help"},
		{"permits", "--help"},
		{"permits", "search", "--help"},
		{"permits", "get", "--help"},
		{"contractors", "--help"},
		{"contractors", "search", "--help"},
		{"contractors", "get", "--help"},
		{"contractors", "permits", "--help"},
		{"contractors", "employees", "--help"},
		{"contractors", "metrics", "--help"},
		{"addresses", "--help"},
		{"addresses", "search", "--help"},
		{"usage", "--help"},
		{"config", "--help"},
		{"config", "set", "--help"},
		{"config", "show", "--help"},
		{"version", "--help"},
	}

	for _, args := range commands {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			result := runCLI(t, args...)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			// Help output should not start with "{" (not JSON).
			trimmed := strings.TrimSpace(result.Stdout)
			if strings.HasPrefix(trimmed, "{") {
				t.Errorf("%s should output plain text, not JSON", strings.Join(args, " "))
			}

			// Stderr should be empty for --help.
			if strings.TrimSpace(result.Stderr) != "" {
				t.Errorf("%s should produce empty stderr, got: %s", strings.Join(args, " "), result.Stderr)
			}
		})
	}
}

// TestHelpUsesConcreteDescriptions verifies that help text uses concrete,
// specific language and avoids generic phrases.
func TestHelpUsesConcreteDescriptions(t *testing.T) {
	// Check root help for concrete resource descriptions.
	result := runCLI(t, "--help")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", result.ExitCode)
	}

	out := result.Stdout

	// Generic phrases that should NOT appear.
	genericPhrases := []string{
		"advanced filtering",
		"various options",
		"powerful tool",
		"easily manage",
	}
	for _, phrase := range genericPhrases {
		if strings.Contains(strings.ToLower(out), strings.ToLower(phrase)) {
			t.Errorf("root --help should not contain generic phrase %q", phrase)
		}
	}

	// Check permits search uses concrete language.
	psResult := runCLI(t, "permits", "search", "--help")
	psOut := psResult.Stdout
	for _, phrase := range genericPhrases {
		if strings.Contains(strings.ToLower(psOut), strings.ToLower(phrase)) {
			t.Errorf("permits search --help should not contain generic phrase %q", phrase)
		}
	}

	// Concrete descriptions should mention specific resource types.
	if !strings.Contains(psOut, "building permits") {
		t.Error("permits search --help should use concrete language like 'building permits'")
	}
}

// TestFlagDescriptionsIncludeValueHints verifies that flag descriptions
// include value hints with format information.
func TestFlagDescriptionsIncludeValueHints(t *testing.T) {
	result := runCLI(t, "permits", "search", "--help")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", result.ExitCode)
	}

	out := result.Stdout

	// --permit-from and --permit-to should mention YYYY-MM-DD format.
	if !strings.Contains(out, "YYYY-MM-DD") {
		t.Error("date flags should include YYYY-MM-DD format hint")
	}

	// --geo-id should include example values.
	if !strings.Contains(out, "ZIP_") {
		t.Error("--geo-id flag should include example like ZIP_90210")
	}

	// Required flags should have "(required)" in their description.
	// Count occurrences of "(required)" - should be at least 3 (geo-id, permit-from, permit-to).
	reqCount := strings.Count(out, "(required)")
	if reqCount < 3 {
		t.Errorf("expected at least 3 required flag markers, found %d", reqCount)
	}
}

// TestContractorsHelpListsSubcommands verifies that `shovels contractors --help`
// lists all five subcommands.
func TestContractorsHelpListsSubcommands(t *testing.T) {
	result := runCLI(t, "contractors", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout
	subcommands := []string{"search", "get", "permits", "employees", "metrics"}
	for _, sub := range subcommands {
		if !strings.Contains(out, sub) {
			t.Errorf("contractors --help should list the %q subcommand", sub)
		}
	}
}

// TestContractorsSearchHelpShowsGroupedFlags verifies that
// `shovels contractors search --help` displays flags grouped by category
// including the contractors-specific Response Options group.
func TestContractorsSearchHelpShowsGroupedFlags(t *testing.T) {
	result := runCLI(t, "contractors", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	// Flag groups must be present.
	groups := []string{"Required Flags", "Permit Filters", "Property Filters", "Contractor Filters", "Response Options"}
	for _, group := range groups {
		if !strings.Contains(out, group) {
			t.Errorf("contractors search --help should contain flag group %q", group)
		}
	}

	// --no-tallies should be present.
	if !strings.Contains(out, "--no-tallies") {
		t.Error("contractors search --help should contain --no-tallies flag")
	}
}

// TestContractorsMetricsHelpShowsRequiredFlags verifies that
// `shovels contractors metrics --help` marks all four flags as required.
func TestContractorsMetricsHelpShowsRequiredFlags(t *testing.T) {
	result := runCLI(t, "contractors", "metrics", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	requiredFlags := []string{"--metric-from", "--metric-to", "--property-type", "--tag"}
	for _, flag := range requiredFlags {
		if !strings.Contains(out, flag) {
			t.Errorf("contractors metrics --help should contain flag %q", flag)
		}
	}

	// All four flags should be marked required.
	reqCount := strings.Count(out, "(required)")
	if reqCount < 4 {
		t.Errorf("contractors metrics --help should mark at least 4 flags as required, found %d", reqCount)
	}
}

// TestAddressesSearchHelpShowsRequiredFlag verifies that
// `shovels addresses search --help` marks the --query flag as required
// and includes usage examples.
func TestAddressesSearchHelpShowsRequiredFlag(t *testing.T) {
	result := runCLI(t, "addresses", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	if !strings.Contains(out, "--query") {
		t.Error("addresses search --help should contain --query flag")
	}
	if !strings.Contains(out, "(required)") {
		t.Error("addresses search --help should mark --query as required")
	}
	if !strings.Contains(out, "123 Main St") {
		t.Error("addresses search --help should include example address")
	}
}
