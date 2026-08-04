//go:build e2e

package e2e

import (
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
