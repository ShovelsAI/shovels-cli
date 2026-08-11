//go:build e2e

package e2e

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// allPropertyTypes lists the 9 API-accepted property type values.
var allPropertyTypes = []string{
	"residential", "commercial", "industrial",
	"agricultural", "vacant land", "exempt",
	"miscellaneous", "office", "recreational",
}

// assertAllPropertyTypes checks that stdout from a help command contains
// every one of the 9 valid property type values.
func assertAllPropertyTypes(t *testing.T, stdout, cmdName string) {
	t.Helper()
	for _, pt := range allPropertyTypes {
		if !strings.Contains(stdout, pt) {
			t.Errorf("%s --help should list property type %q", cmdName, pt)
		}
	}
}

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
	commands := []string{"permits", "properties", "contractors", "addresses", "cities", "counties", "jurisdictions", "zipcodes", "states", "tags", "usage", "config", "version"}
	for _, cmd := range commands {
		if !strings.Contains(out, cmd) {
			t.Errorf("root --help should list the %q command", cmd)
		}
	}

	// Global flags must be present. Root advertises the full API-only set:
	// it is where they are registered and where an agent discovers them.
	globalFlags := []string{
		"--limit",
		"--max-records",
		"--base-url",
		"--no-retry",
		"--timeout",
		"--dry-run",
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
	if !strings.Contains(out, "92024") {
		t.Error("permits search --help should contain example zip code 92024")
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

	// All 9 property types must be listed in help text.
	assertAllPropertyTypes(t, out, "permits search")

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
		{"cities", "--help"},
		{"cities", "search", "--help"},
		{"counties", "--help"},
		{"counties", "search", "--help"},
		{"jurisdictions", "--help"},
		{"jurisdictions", "search", "--help"},
		{"zipcodes", "--help"},
		{"zipcodes", "search", "--help"},
		{"states", "--help"},
		{"states", "search", "--help"},
		{"tags", "--help"},
		{"tags", "list", "--help"},
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
	if !strings.Contains(out, "92024") {
		t.Error("--geo-id flag should include example zip code like 92024")
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

	// All 9 property types must be listed in help text.
	assertAllPropertyTypes(t, out, "contractors search")

	// Help example should use full classification value, not truncated.
	if !strings.Contains(out, "general_building_contractor") {
		t.Error("contractors search --help example should use general_building_contractor")
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

	// All 9 property types must be listed in help text.
	assertAllPropertyTypes(t, out, "contractors metrics")
}

func TestContractorsMetricsHelpDescribesCreditExemptPagination(t *testing.T) {
	result := runCLI(t, "contractors", "metrics", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout
	if !strings.Contains(out, "cursor-paginated") {
		t.Error("contractors metrics --help should describe cursor pagination")
	}
	if !strings.Contains(out, `"count": N, "has_more": bool`) {
		t.Error("contractors metrics --help should describe pagination metadata")
	}
	if !strings.Contains(out, "credit-exempt") {
		t.Error("contractors metrics --help should describe the endpoint as credit-exempt")
	}
	if strings.Contains(out, "Metrics are not paginated") {
		t.Error("contractors metrics --help should not describe the endpoint as unpaginated")
	}
}

func TestContractorsMetricsHelpListsAllPropertyType(t *testing.T) {
	result := runCLI(t, "contractors", "metrics", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "recreational, all") {
		t.Error("contractors metrics --help should list property type all")
	}
}

// TestCitiesMetricsHelpListsAllPropertyTypes verifies that
// cities metrics current and monthly help text lists all 9 property types.
func TestCitiesMetricsHelpListsAllPropertyTypes(t *testing.T) {
	for _, sub := range []string{"current", "monthly"} {
		t.Run(sub, func(t *testing.T) {
			result := runCLI(t, "cities", "metrics", sub, "--help")
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}
			assertAllPropertyTypes(t, result.Stdout, "cities metrics "+sub)
		})
	}
}

// TestCountiesMetricsHelpListsAllPropertyTypes verifies that
// counties metrics current and monthly help text lists all 9 property types.
func TestCountiesMetricsHelpListsAllPropertyTypes(t *testing.T) {
	for _, sub := range []string{"current", "monthly"} {
		t.Run(sub, func(t *testing.T) {
			result := runCLI(t, "counties", "metrics", sub, "--help")
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}
			assertAllPropertyTypes(t, result.Stdout, "counties metrics "+sub)
		})
	}
}

// TestJurisdictionsMetricsHelpListsAllPropertyTypes verifies that
// jurisdictions metrics current and monthly help text lists all 9 property types.
func TestJurisdictionsMetricsHelpListsAllPropertyTypes(t *testing.T) {
	for _, sub := range []string{"current", "monthly"} {
		t.Run(sub, func(t *testing.T) {
			result := runCLI(t, "jurisdictions", "metrics", sub, "--help")
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}
			assertAllPropertyTypes(t, result.Stdout, "jurisdictions metrics "+sub)
		})
	}
}

// TestRootHelpRecommendsCoverageInWorkflow verifies that `shovels --help`
// includes a recommended-workflow block naming coverage as the credit-exempt
// step to run before search or metrics.
func TestRootHelpRecommendsCoverageInWorkflow(t *testing.T) {
	result := runCLI(t, "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	if !strings.Contains(out, "Recommended workflow") {
		t.Error("root --help should include a Recommended workflow block")
	}
	if !strings.Contains(out, "coverage") {
		t.Error("root --help workflow should name the coverage command")
	}
	if !strings.Contains(out, "credit-exempt") {
		t.Error("root --help workflow should frame coverage as credit-exempt")
	}
}

// TestPermitsSearchHelpShowsCoverageTip verifies that
// `shovels permits search --help` includes a directive pointing to the
// credit-exempt coverage command as a pre-flight check, and that the
// directive appears BEFORE the required-flag content so imperative-task
// agents see it before their first query.
func TestPermitsSearchHelpShowsCoverageTip(t *testing.T) {
	result := runCLI(t, "permits", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	coverageIdx := strings.Index(out, "coverage GEO_ID")
	if coverageIdx == -1 {
		t.Fatal("permits search --help should point to `<geo> coverage GEO_ID`")
	}
	if !strings.Contains(out, "credit-exempt") {
		t.Error("permits search --help coverage directive should mention credit-exempt")
	}

	requiredIdx := strings.Index(out, "Required flags:")
	if requiredIdx == -1 {
		t.Fatal("permits search --help should contain a Required flags section")
	}
	if coverageIdx > requiredIdx {
		t.Error("permits search --help coverage directive should appear before the Required flags section")
	}
}

// TestContractorsSearchHelpShowsCoverageTip verifies that
// `shovels contractors search --help` includes a directive pointing to the
// credit-exempt coverage command as a pre-flight check, and that the
// directive appears BEFORE the required-flag content so imperative-task
// agents see it before their first query.
func TestContractorsSearchHelpShowsCoverageTip(t *testing.T) {
	result := runCLI(t, "contractors", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	coverageIdx := strings.Index(out, "coverage GEO_ID")
	if coverageIdx == -1 {
		t.Fatal("contractors search --help should point to `<geo> coverage GEO_ID`")
	}
	if !strings.Contains(out, "credit-exempt") {
		t.Error("contractors search --help coverage directive should mention credit-exempt")
	}

	requiredIdx := strings.Index(out, "Required flags:")
	if requiredIdx == -1 {
		t.Fatal("contractors search --help should contain a Required flags section")
	}
	if coverageIdx > requiredIdx {
		t.Error("contractors search --help coverage directive should appear before the Required flags section")
	}
}

// TestMetricsHelpShowsCoverageTip verifies that the metrics command help on
// cities, counties, and jurisdictions points to the credit-exempt coverage
// command.
func TestMetricsHelpShowsCoverageTip(t *testing.T) {
	for _, geo := range []string{"cities", "counties", "jurisdictions"} {
		t.Run(geo, func(t *testing.T) {
			result := runCLI(t, geo, "metrics", "--help")
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			out := result.Stdout
			if !strings.Contains(out, geo+" coverage") {
				t.Errorf("%s metrics --help should point to %q", geo, geo+" coverage")
			}
			if !strings.Contains(out, "credit-exempt") {
				t.Errorf("%s metrics --help coverage tip should mention credit-exempt", geo)
			}
		})
	}
}

// TestCoverageHelpExplainsBothCauses verifies that `<geo> coverage --help`
// explains both reasons a permit field can be unreliable: permits_total == 0
// (no data for the window — jurisdiction not covered or no permits filed) and
// permits_total > 0 with a low fill_pct (the source jurisdiction does not
// populate the field). It also states that an absent field is reliable.
func TestCoverageHelpExplainsBothCauses(t *testing.T) {
	for _, geo := range []string{"cities", "counties", "jurisdictions", "states", "zipcodes"} {
		t.Run(geo, func(t *testing.T) {
			result := runCLI(t, geo, "coverage", "--help")
			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			out := result.Stdout

			// Cause 1: permits_total == 0 means no data for the window, and
			// the help must say the two sub-causes are not distinguishable.
			if !strings.Contains(out, "permits_total == 0") {
				t.Error("coverage --help should explain the permits_total == 0 cause")
			}
			if !strings.Contains(out, "no permits") {
				t.Error("coverage --help should note permits_total == 0 may mean no permits were filed")
			}
			// The ambiguity itself: permits_total == 0 cannot tell "not
			// covered" apart from "no permits filed in the window".
			if !strings.Contains(out, "does NOT distinguish") {
				t.Error("coverage --help should state permits_total == 0 does NOT distinguish the two sub-causes")
			}
			if !strings.Contains(out, "the two are indistinguishable") {
				t.Error("coverage --help should state the two sub-causes are indistinguishable")
			}

			// Cause 2: permits_total > 0 with low fill_pct means the source
			// jurisdiction does not populate the field.
			if !strings.Contains(out, "permits_total > 0") {
				t.Error("coverage --help should explain the permits_total > 0 / field-not-sourced cause")
			}
			if !strings.Contains(out, "do not populate that field") {
				t.Error("coverage --help should say the source jurisdiction does not populate the field")
			}

			// Absent field is reliable.
			if !strings.Contains(out, "absent from the data array is reliable") {
				t.Error("coverage --help should state an absent field is reliable")
			}
		})
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

// TestPermitsSearchHelpShowsGeoResolutionCommands verifies that
// `shovels permits search --help` lists cities search, counties search,
// and jurisdictions search as geo_id resolution commands.
func TestPermitsSearchHelpShowsGeoResolutionCommands(t *testing.T) {
	result := runCLI(t, "permits", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	resolutionCommands := []string{
		"cities search",
		"counties search",
		"jurisdictions search",
		"addresses search",
	}
	for _, cmd := range resolutionCommands {
		if !strings.Contains(out, cmd) {
			t.Errorf("permits search --help should reference %q as a resolution command", cmd)
		}
	}
}

// TestContractorsSearchHelpShowsGeoResolutionCommands verifies that
// `shovels contractors search --help` lists cities search, counties search,
// and jurisdictions search as geo_id resolution commands.
func TestContractorsSearchHelpShowsGeoResolutionCommands(t *testing.T) {
	result := runCLI(t, "contractors", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	resolutionCommands := []string{
		"cities search",
		"counties search",
		"jurisdictions search",
		"addresses search",
	}
	for _, cmd := range resolutionCommands {
		if !strings.Contains(out, cmd) {
			t.Errorf("contractors search --help should reference %q as a resolution command", cmd)
		}
	}
}

// TestGeoIDFlagShowsAllResolutionCommands verifies that the --geo-id
// flag description in the flags section shows all three geo resolution
// commands (cities, counties, jurisdictions) plus addresses.
func TestGeoIDFlagShowsAllResolutionCommands(t *testing.T) {
	result := runCLI(t, "permits", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	out := result.Stdout

	// The --geo-id flag description should list resolution commands.
	resolutionLines := []string{
		"cities search",
		"counties search",
		"jurisdictions search",
		"addresses search",
	}
	for _, line := range resolutionLines {
		if !strings.Contains(out, line) {
			t.Errorf("--geo-id flag description should reference %q", line)
		}
	}

	// Must show correct geo_id formats (bare zip codes, state codes).
	if !strings.Contains(out, "92024") {
		t.Error("--geo-id should show bare zip code example like 92024")
	}
	if !strings.Contains(out, "CA") {
		t.Error("--geo-id should show state code example like CA")
	}
}

// TestPropertiesSearchHelp verifies that `shovels properties search --help`
// carries everything a blind agent needs before its first call: the Beta
// instability warning, the ZIP scopes properties accepts where decisions does
// not, the absence-tag syntax and its trust metadata, the nationwide
// owner-portfolio path, the geo_id resolution commands plus the jurisdiction
// caveat, the absence of a --permit-to flag, the grouped-flag layout, the
// property attribute filters with their units and coverage caveat, and the
// timed-out total_count caveat.
func TestPropertiesSearchHelp(t *testing.T) {
	result := runCLI(t, "properties", "search", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	out := result.Stdout

	cases := []struct {
		name    string
		wants   []string
		purpose string
	}{
		{
			name:    "BetaWarning",
			wants:   []string{"Beta", "unstable"},
			purpose: "label the endpoint Beta and warn the response shape is unstable",
		},
		{
			name:    "ZipScopes",
			wants:   []string{"92024", "92024-1234"},
			purpose: "show that bare ZIP and ZIP+4 scopes work directly",
		},
		{
			name:    "AbsenceTagSyntax",
			wants:   []string{`--permit-tags "-solar"`, "trust"},
			purpose: "teach the -prefixed absence syntax and the trust metadata it returns",
		},
		{
			name:    "OwnerPortfolio",
			wants:   []string{"--legal-owner", "nationwide"},
			purpose: "show that --legal-owner alone searches an owner nationwide",
		},
		{
			name:    "GeoResolutionCommands",
			wants:   []string{"cities search", "counties search", "addresses search", "Jurisdiction"},
			purpose: "point at the geo_id resolution commands and flag the jurisdiction rejection",
		},
		{
			name:    "DateWindowRedirect",
			wants:   []string{"no --permit-to", "shovels permits search"},
			purpose: "state that no --permit-to flag exists and redirect date windows to permits search",
		},
		{
			name:    "GroupedFlags",
			wants:   []string{"Required Scope", "Permit Filters:", "Property Filters:", "Response Options:"},
			purpose: "render the grouped-flag layout so scope flags are distinguishable from filters",
		},
		{
			name: "PropertyAttributeFilters",
			wants: []string{
				"--property-type",
				"--property-min-market-value", "--property-max-market-value",
				"--property-min-lot-size", "--property-max-lot-size",
				"--property-min-building-area", "--property-max-building-area",
				"--property-min-unit-count", "--property-max-unit-count",
				"--property-min-year-built", "--property-max-year-built",
			},
			purpose: "list the type filter and both bounds of all five attribute range pairs",
		},
		{
			name:    "PropertyTypeValues",
			wants:   []string{"residential", "commercial", "industrial", "agricultural", "vacant land", "exempt", "miscellaneous", "office", "recreational"},
			purpose: "name all nine valid --property-type values so an agent never has to guess one",
		},
		{
			name:    "AttributeUnits",
			wants:   []string{"integer cents", "square feet"},
			purpose: "state the units the attribute range filters expect",
		},
		{
			name:    "AttributeCoverageCaveat",
			wants:   []string{"60-70%", "narrows"},
			purpose: "warn that attribute data is partial, so a range filter narrows to the covered set",
		},
		{
			name:    "TimedOutCount",
			wants:   []string{"total_count", "times out"},
			purpose: "warn that an omitted total_count can mean the count query timed out, not zero results",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("properties search --help should %s (missing %q)", tc.purpose, want)
				}
			}
		})
	}

	// Permits search filters on story count; the Properties API has no such
	// parameter, so help must not offer one.
	t.Run("NoStoryCountFilter", func(t *testing.T) {
		if strings.Contains(out, "story-count") {
			t.Error("properties search --help must not offer a story-count filter")
		}
	})
}

// TestPropertiesGetHelp verifies that `shovels properties get --help` carries
// what an agent needs before its first batch lookup: the Beta instability
// warning, positional-argument usage with a correct and an incorrect example,
// the ID count bound, where address IDs come from, the rejection of
// city/county/jurisdiction IDs, and the meaning of meta.missing.
func TestPropertiesGetHelp(t *testing.T) {
	result := runCLI(t, "properties", "get", "--help")

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	out := result.Stdout

	cases := []struct {
		name    string
		wants   []string
		purpose string
	}{
		{
			name:    "BetaWarning",
			wants:   []string{"Beta", "unstable"},
			purpose: "label the endpoint Beta and warn the response shape is unstable",
		},
		{
			name:    "PositionalUsage",
			wants:   []string{"positional argument, not a flag", "Correct:   shovels properties get a_123", "Incorrect: shovels properties get --id a_123"},
			purpose: "show that IDs are positional and that --id is wrong",
		},
		{
			name:    "UseLine",
			wants:   []string{"properties get ID [ID...]"},
			purpose: "show the positional syntax in the usage line",
		},
		{
			name:    "IDCountBound",
			wants:   []string{"1 to 50"},
			purpose: "state how many IDs one request accepts",
		},
		{
			name:    "AddressIDsOnly",
			wants:   []string{"address IDs", "addresses search", "jurisdiction"},
			purpose: "state that only address IDs work and point at the command that resolves one",
		},
		{
			name:    "MissingIDs",
			wants:   []string{"meta.missing"},
			purpose: "explain where unresolved IDs surface in the response",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, want := range tc.wants {
				if !strings.Contains(out, want) {
					t.Errorf("properties get --help should %s (missing %q)", tc.purpose, want)
				}
			}
		})
	}
}

// apiOnlyFlags are the global flags each command's help advertises only if it
// honors them. --base-url is absent: every command that resolves config honors
// it.
var apiOnlyFlags = []string{"--limit", "--max-records", "--no-retry", "--timeout", "--dry-run"}

// helpText runs a command's --help and returns everything it printed.
func helpText(t *testing.T, args ...string) string {
	t.Helper()

	result := runCLI(t, append(args, "--help")...)
	if result.ExitCode != 0 {
		t.Fatalf("%v --help: expected exit 0, got %d; stderr: %s", args, result.ExitCode, result.Stderr)
	}
	return result.Stdout
}

// globalFlagsHelp returns a command's Global Flags section, which the sections
// after it are separated from by a blank line. Asserting against the section
// rather than the whole output keeps a flag named in a description or an
// example from reading as an advertised flag.
func globalFlagsHelp(t *testing.T, args ...string) string {
	t.Helper()

	help := helpText(t, args...)
	_, after, found := strings.Cut(help, "\nGlobal Flags:\n")
	if !found {
		t.Fatalf("%v --help has no Global Flags section:\n%s", args, help)
	}
	section, _, _ := strings.Cut(after, "\n\n")
	return section
}

// assertAdvertisesExactly checks the Global Flags section lists every named
// API-only flag and none of the others. A flag is advertised when it opens a
// flag line; the flags have no shorthand, so pflag indents each by six spaces.
func assertAdvertisesExactly(t *testing.T, section, cmdName string, advertised ...string) {
	t.Helper()

	for _, flag := range apiOnlyFlags {
		want := slices.Contains(advertised, flag)
		got := strings.Contains("\n"+section, "\n      "+flag)
		if want && !got {
			t.Errorf("%s --help should advertise %s, got:\n%s", cmdName, flag, section)
		}
		if !want && got {
			t.Errorf("%s --help should not advertise %s, got:\n%s", cmdName, flag, section)
		}
	}
}

// --- Happy paths ---

func TestConfigSetHelpAdvertisesDryRunAndBaseURLOnly(t *testing.T) {
	section := globalFlagsHelp(t, "config", "set")

	assertAdvertisesExactly(t, section, "config set", "--dry-run")
	if !strings.Contains(section, "--base-url") {
		t.Errorf("config set --help should advertise --base-url, got:\n%s", section)
	}
}

func TestPermitsSearchHelpAdvertisesEveryAPIOnlyFlag(t *testing.T) {
	section := globalFlagsHelp(t, "permits", "search")

	assertAdvertisesExactly(t, section, "permits search", apiOnlyFlags...)
}

func TestContractorsMetricsHelpAdvertisesTransportFlagsOnly(t *testing.T) {
	section := globalFlagsHelp(t, "contractors", "metrics")

	assertAdvertisesExactly(t, section, "contractors metrics", "--no-retry", "--timeout", "--dry-run")
}

// --- Edge cases ---

// The search commands render inherited flags through writeGroupedUsage instead
// of cobra's usage template — permits search above included. They honor all
// five, so this is what proves the grouped path advertises no less than the
// default one.
func TestGroupedSearchHelpAdvertisesEveryAPIOnlyFlag(t *testing.T) {
	for _, cmdArgs := range [][]string{
		{"contractors", "search"},
		{"properties", "search"},
		{"decisions", "search"},
	} {
		t.Run(strings.Join(cmdArgs, " "), func(t *testing.T) {
			section := globalFlagsHelp(t, cmdArgs...)

			assertAdvertisesExactly(t, section, strings.Join(cmdArgs, " "), apiOnlyFlags...)
		})
	}
}

// Every real grouped command honors all five, so an unfiltered grouped renderer
// produces their sections unchanged. _test-paginate renders through the same
// path with a contract missing --dry-run, which is what makes that section
// differ between a filtered and an unfiltered renderer.
func TestGroupedFixtureHelpOmitsTheUnhonoredDryRun(t *testing.T) {
	section := globalFlagsHelp(t, "_test-paginate")

	assertAdvertisesExactly(t, section, "_test-paginate", "--limit", "--max-records", "--no-retry", "--timeout")
}

func TestLocalCommandHelpAdvertisesNoAPIOnlyFlag(t *testing.T) {
	for _, cmdArgs := range [][]string{
		{"version"},
		{"schema"},
		{"completion"},
		{"config", "show"},
	} {
		t.Run(strings.Join(cmdArgs, " "), func(t *testing.T) {
			section := globalFlagsHelp(t, cmdArgs...)

			assertAdvertisesExactly(t, section, strings.Join(cmdArgs, " "))
		})
	}
}

// The completion leaves are generated by cobra during Execute rather than
// declared in cmd/, and they are user-facing commands all the same.
func TestCompletionLeafHelpAdvertisesNoAPIOnlyFlag(t *testing.T) {
	section := globalFlagsHelp(t, "completion", "bash")

	assertAdvertisesExactly(t, section, "completion bash")
}

// --- Boundary conditions ---

// A parent runs no code of its own, so what it advertises is what naming one of
// its subcommands can reach: contractors reaches all five, config only --dry-run.
func TestParentHelpAdvertisesTheUnionOfItsSubcommands(t *testing.T) {
	assertAdvertisesExactly(t, globalFlagsHelp(t, "contractors"), "contractors", apiOnlyFlags...)
	assertAdvertisesExactly(t, globalFlagsHelp(t, "config"), "config", "--dry-run")
}

// flagDeclarations maps each flag a help text declares to the line declaring
// it, which is where the flag's description is published. Only the sections
// after the usage line are read: before it is prose, where a flag can be named
// in an example without the command accepting it.
func flagDeclarations(help string) map[string]string {
	declarations := map[string]string{}

	_, sections, found := strings.Cut(help, "\nUsage:\n")
	if !found {
		return declarations
	}
	for _, line := range strings.Split(sections, "\n") {
		if !strings.HasPrefix(line, " ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		// A shorthand precedes the long form as "-q, --query".
		if len(fields) > 1 && strings.HasPrefix(fields[0], "-") && strings.HasSuffix(fields[0], ",") {
			fields = fields[1:]
		}
		if strings.HasPrefix(fields[0], "--") {
			declarations[fields[0]] = line
		}
	}
	return declarations
}

// flagHelpLine returns the line a command's help declares the named flag on.
// Root declares the five under "Flags:" and every other command inherits them
// under "Global Flags:", so the section header is not what locates them.
func flagHelpLine(t *testing.T, flag string, args ...string) string {
	t.Helper()

	line, ok := flagDeclarations(helpText(t, args...))[flag]
	if !ok {
		t.Fatalf("%v --help declares no %s", args, flag)
	}
	return line
}

// assertContainsAll checks the text carries every wanted substring, naming the
// claim each one stands for.
func assertContainsAll(t *testing.T, text, subject string, wants ...string) {
	t.Helper()

	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("%s should state %q, got:\n%s", subject, want, text)
		}
	}
}

// --- Pagination contract: happy paths ---

// Each capped search publishes its own cap rather than a generic "capped", so
// an agent knows how many rows the query can ever return. Every other measured
// cap must be absent, or one shared number would satisfy all four.
func TestCappedSearchHelpStatesItsOwnCapAndNoPagination(t *testing.T) {
	for _, search := range cappedSearches() {
		t.Run(search.name, func(t *testing.T) {
			out := helpText(t, strings.Fields(search.name)...)

			assertContainsAll(t, out, search.name+" --help",
				"not paginated", "no continuation cursor",
				fmt.Sprintf("at most %d results", search.cap))

			for _, other := range cappedSearches() {
				if other.cap == search.cap {
					continue
				}
				stated := fmt.Sprintf("at most %d results", other.cap)
				if strings.Contains(out, stated) {
					t.Errorf("%s --help should not state %q, which is %s's cap", search.name, stated, other.name)
				}
			}
		})
	}
}

// permits search follows a real cursor, so --limit bounds the records collected
// across pages and no cap sentence applies to it.
func TestPaginatedSearchHelpStatesTheCursorLimitAndNoCap(t *testing.T) {
	line := flagHelpLine(t, "--limit", "permits", "search")
	out := helpText(t, "permits", "search")

	assertContainsAll(t, line, "permits search --limit", "Maximum records to return on a cursor-paginated command")
	for _, capped := range []string{"at most 20 results", "at most 15 results", "not paginated"} {
		if strings.Contains(out, capped) {
			t.Errorf("permits search --help should not state %q", capped)
		}
	}
}

// --- Pagination contract: edge cases ---

// One flag object carries one description, so root's --limit text has to hold
// for both classes that honor the flag. Naming only the cursor class would
// contradict the capped searches, which keep --limit and state their own bound.
func TestRootLimitDescriptionCoversBothClassesThatHonorIt(t *testing.T) {
	line := flagHelpLine(t, "--limit")

	assertContainsAll(t, line, "root --limit",
		"cursor-paginated command or a capped search",
		"stops at the cap its own help states")
}

// rootExample is one `shovels ...` invocation embedded in root's help: the
// command path it names and the long flags it passes to it.
type rootExample struct {
	path  string
	flags []string
}

// rootExamples extracts the invocations from a help text. A segment runs from
// "shovels " to the end of its line or the first pipe, its path is the run of
// command-shaped words that opens it, and its flags are the long flags that
// follow. An invocation opening on a placeholder ("shovels <geo> coverage")
// names no path to run and is left out.
func rootExamples(help string) []rootExample {
	var examples []rootExample

	for _, line := range strings.Split(help, "\n") {
		for _, segment := range strings.Split(line, "shovels ")[1:] {
			segment, _, _ = strings.Cut(segment, "|")

			var path []string
			var flags []string
			inPath := true
			for _, token := range strings.Fields(segment) {
				if flag, isFlag := strings.CutPrefix(token, "--"); isFlag {
					name, _, _ := strings.Cut(flag, "=")
					flags = append(flags, "--"+name)
					inPath = false
					continue
				}
				if inPath && isBareWord(token) {
					path = append(path, token)
					continue
				}
				inPath = false
			}
			if len(path) > 0 {
				examples = append(examples, rootExample{path: strings.Join(path, " "), flags: flags})
			}
		}
	}
	return examples
}

// isBareWord reports whether a token has the shape of a command name: lowercase
// letters and dashes, which no placeholder, quoted value or shell fragment in
// these examples has. A positional argument of that shape joins the path, and
// cobra renders the same command's help either way.
func isBareWord(token string) bool {
	for _, r := range token {
		if (r < 'a' || r > 'z') && r != '-' {
			return false
		}
	}
	return token != ""
}

// An example passing a flag its command does not accept is a documented failure:
// a command outside the flag's contract rejects it, and an unregistered flag
// never parses. Both show up as the flag being absent from that command's own
// flag sections.
func TestRootHelpExamplesPassOnlyFlagsTheirCommandDeclares(t *testing.T) {
	checked := map[string][]string{}

	for _, example := range rootExamples(helpText(t)) {
		result := runCLI(t, append(strings.Fields(example.path), "--help")...)
		if result.ExitCode != 0 {
			// Prose reads as an invocation too ("shovels is a CLI for ...").
			continue
		}

		declared := flagDeclarations(result.Stdout)
		for _, flag := range example.flags {
			if _, ok := declared[flag]; !ok {
				t.Errorf("root --help example runs %q with %s, which that command does not declare", example.path, flag)
			}
			checked[example.path] = append(checked[example.path], flag)
		}
	}

	for path, flag := range map[string]string{
		"permits search":     "--geo-id",
		"contractors search": "--tags",
	} {
		if !slices.Contains(checked[path], flag) {
			t.Errorf("expected the examples to cover %q with %s, checked %v", path, flag, checked)
		}
	}
}
