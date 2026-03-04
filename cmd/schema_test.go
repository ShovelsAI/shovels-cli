package cmd

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestGeneratedSchemaCoversAllDataCommands verifies the schema registry
// contains entries for all data commands (permits, contractors, geo, tags).
func TestGeneratedSchemaCoversAllDataCommands(t *testing.T) {
	expected := []string{
		"permits search",
		"permits get",
		"contractors search",
		"contractors get",
		"contractors permits",
		"contractors employees",
		"contractors metrics",
		"cities search",
		"cities metrics current",
		"cities metrics monthly",
		"counties search",
		"counties metrics current",
		"counties metrics monthly",
		"jurisdictions search",
		"jurisdictions metrics current",
		"jurisdictions metrics monthly",
		"addresses search",
		"addresses metrics current",
		"addresses metrics monthly",
		"addresses residents",
		"zipcodes search",
		"states search",
		"tags list",
	}

	for _, cmd := range expected {
		if LookupSchema(cmd) == nil {
			t.Errorf("schema registry missing entry for %q", cmd)
		}
	}
}

// TestSchemaVersionIsOne verifies every schema has schema_version = 1.
func TestSchemaVersionIsOne(t *testing.T) {
	for _, cmd := range SchemaCommands() {
		s := LookupSchema(cmd)
		if s.SchemaVersion != 1 {
			t.Errorf("schema for %q has version %d, expected 1", cmd, s.SchemaVersion)
		}
	}
}

// TestCommandPathsAreSpaceSeparated verifies all command paths use spaces,
// not hyphens or slashes.
func TestCommandPathsAreSpaceSeparated(t *testing.T) {
	for _, cmd := range SchemaCommands() {
		if strings.Contains(cmd, "-") || strings.Contains(cmd, "/") {
			t.Errorf("command path %q should use spaces, not hyphens or slashes", cmd)
		}
	}
}

// TestSchemaCommandFieldMatchesPKey verifies each schema's Command field
// matches the map key it's stored under.
func TestSchemaCommandFieldMatchesKey(t *testing.T) {
	for _, cmd := range SchemaCommands() {
		s := LookupSchema(cmd)
		if s.Command != cmd {
			t.Errorf("schema key %q has Command field %q", cmd, s.Command)
		}
	}
}

// TestSchemaHasResponseFields verifies every schema has at least one
// response field (no empty schemas).
func TestSchemaHasResponseFields(t *testing.T) {
	for _, cmd := range SchemaCommands() {
		s := LookupSchema(cmd)
		if len(s.ResponseFields) == 0 {
			t.Errorf("schema for %q has no response fields", cmd)
		}
	}
}

// TestSchemaHasFieldIndex verifies every schema has a non-empty field index.
func TestSchemaHasFieldIndex(t *testing.T) {
	for _, cmd := range SchemaCommands() {
		s := LookupSchema(cmd)
		if len(s.FieldIndex) == 0 {
			t.Errorf("schema for %q has no field index entries", cmd)
		}
	}
}

// TestSchemaFieldIndexContainsMetaFields verifies every schema's field index
// includes the standard meta fields used for pagination and credit tracking.
func TestSchemaFieldIndexContainsMetaFields(t *testing.T) {
	metaFields := []string{"meta.count", "meta.has_more", "meta.credits_used", "meta.credits_remaining"}

	for _, cmd := range SchemaCommands() {
		s := LookupSchema(cmd)
		indexSet := make(map[string]bool, len(s.FieldIndex))
		for _, f := range s.FieldIndex {
			indexSet[f] = true
		}
		for _, mf := range metaFields {
			if !indexSet[mf] {
				t.Errorf("schema for %q missing meta field %q in field_index", cmd, mf)
			}
		}
	}
}

// TestSchemaFieldIndexPrefixedWithData verifies response field entries in
// the field index use the "data[]." prefix for jq compatibility.
func TestSchemaFieldIndexPrefixedWithData(t *testing.T) {
	for _, cmd := range SchemaCommands() {
		s := LookupSchema(cmd)
		for _, f := range s.FieldIndex {
			if !strings.HasPrefix(f, "data[].") && !strings.HasPrefix(f, "meta.") {
				t.Errorf("schema for %q has field index entry %q without data[]. or meta. prefix", cmd, f)
			}
		}
	}
}

// TestLookupSchemaReturnsNilForUnknown verifies that looking up a
// non-existent command returns nil.
func TestLookupSchemaReturnsNilForUnknown(t *testing.T) {
	if s := LookupSchema("foobar nonsense"); s != nil {
		t.Error("expected nil for unknown command path")
	}
}

// TestSchemaCommandsReturnsSortedList verifies SchemaCommands returns
// alphabetically sorted command paths.
func TestSchemaCommandsReturnsSortedList(t *testing.T) {
	cmds := SchemaCommands()
	for i := 1; i < len(cmds); i++ {
		if cmds[i] < cmds[i-1] {
			t.Errorf("SchemaCommands not sorted: %q before %q", cmds[i-1], cmds[i])
		}
	}
}

// TestMergeFieldOverridesTakePrecedence verifies that override values
// replace OpenAPI-derived values.
func TestMergeFieldOverridesTakePrecedence(t *testing.T) {
	base := SchemaField{
		Type:        "integer",
		Description: "OpenAPI description",
	}
	override := OverrideField{
		Description: "Enriched description",
		Unit:        "dollars",
		Range:       "0-100",
	}

	result := MergeField(base, override)

	if result.Description != "Enriched description" {
		t.Errorf("expected enriched description, got %q", result.Description)
	}
	if result.Unit != "dollars" {
		t.Errorf("expected unit 'dollars', got %q", result.Unit)
	}
	if result.Range != "0-100" {
		t.Errorf("expected range '0-100', got %q", result.Range)
	}
	if result.Type != "integer" {
		t.Errorf("expected type 'integer' preserved, got %q", result.Type)
	}
}

// TestMergeFieldPreservesBaseWhenOverrideEmpty verifies that empty override
// fields do not overwrite base values (OpenAPI description preserved).
func TestMergeFieldPreservesBaseWhenOverrideEmpty(t *testing.T) {
	base := SchemaField{
		Type:        "string",
		Description: "OpenAPI description",
		Unit:        "days",
	}
	override := OverrideField{}

	result := MergeField(base, override)

	if result.Description != "OpenAPI description" {
		t.Errorf("expected base description preserved, got %q", result.Description)
	}
	if result.Unit != "days" {
		t.Errorf("expected base unit preserved, got %q", result.Unit)
	}
}

// TestMergeFieldsIgnoresPhantomFields verifies that override fields not
// present in the base map are silently ignored (no phantom fields).
func TestMergeFieldsIgnoresPhantomFields(t *testing.T) {
	base := map[string]SchemaField{
		"real_field": {Type: "string", Description: "exists"},
	}
	overrides := map[string]OverrideField{
		"real_field":    {Description: "enriched"},
		"phantom_field": {Description: "should be ignored"},
	}

	result := MergeFields(base, overrides)

	if _, ok := result["phantom_field"]; ok {
		t.Error("phantom field should not appear in merged result")
	}
	if result["real_field"].Description != "enriched" {
		t.Errorf("expected real_field description 'enriched', got %q", result["real_field"].Description)
	}
}

// TestFieldWithoutOverrideUsesOpenAPIDescription verifies that fields
// without overrides retain their OpenAPI-derived descriptions.
func TestFieldWithoutOverrideUsesOpenAPIDescription(t *testing.T) {
	base := map[string]SchemaField{
		"field_a": {Type: "string", Description: "from openapi"},
		"field_b": {Type: "integer", Description: "also from openapi"},
	}
	overrides := map[string]OverrideField{
		"field_a": {Description: "enriched a"},
	}

	result := MergeFields(base, overrides)

	if result["field_a"].Description != "enriched a" {
		t.Errorf("field_a should be enriched, got %q", result["field_a"].Description)
	}
	if result["field_b"].Description != "also from openapi" {
		t.Errorf("field_b should retain OpenAPI description, got %q", result["field_b"].Description)
	}
}

// TestSchemaLookupDeterministic verifies that repeated lookups return
// identical schemas (deterministic, since data is embedded at build time).
func TestSchemaLookupDeterministic(t *testing.T) {
	s1 := LookupSchema("permits search")
	s2 := LookupSchema("permits search")

	if s1 == nil || s2 == nil {
		t.Fatal("permits search schema should exist")
	}
	if s1.SchemaVersion != s2.SchemaVersion {
		t.Error("schema version mismatch between lookups")
	}
	if s1.Command != s2.Command {
		t.Error("command mismatch between lookups")
	}
	if len(s1.ResponseFields) != len(s2.ResponseFields) {
		t.Error("response field count mismatch between lookups")
	}
}

// TestOverridesYAMLParsesSuccessfully verifies the YAML overrides file
// parses without errors and contains expected command entries.
func TestOverridesYAMLParsesSuccessfully(t *testing.T) {
	data, err := os.ReadFile("schema_overrides.yaml")
	if err != nil {
		t.Fatalf("failed to read schema_overrides.yaml: %v", err)
	}

	var overrides map[string]OverrideCommand
	if err := yaml.Unmarshal(data, &overrides); err != nil {
		t.Fatalf("failed to parse schema_overrides.yaml: %v", err)
	}

	expectedCommands := []string{
		"permits search",
		"contractors search",
		"cities metrics current",
		"tags list",
		"addresses residents",
	}
	for _, cmd := range expectedCommands {
		if _, ok := overrides[cmd]; !ok {
			t.Errorf("overrides missing entry for %q", cmd)
		}
	}
}

// TestOverridesOnlyContainValidCommands verifies every command in the
// overrides file maps to a registered schema command.
func TestOverridesOnlyContainValidCommands(t *testing.T) {
	data, err := os.ReadFile("schema_overrides.yaml")
	if err != nil {
		t.Fatalf("failed to read schema_overrides.yaml: %v", err)
	}

	var overrides map[string]OverrideCommand
	if err := yaml.Unmarshal(data, &overrides); err != nil {
		t.Fatalf("failed to parse schema_overrides.yaml: %v", err)
	}

	validCommands := make(map[string]bool)
	for _, cmd := range SchemaCommands() {
		validCommands[cmd] = true
	}

	for cmd := range overrides {
		if !validCommands[cmd] {
			t.Errorf("override for %q does not match any registered schema command", cmd)
		}
	}
}

// TestSearchCommandsHaveAllSharedFilters verifies that permits search and
// contractors search schemas include every filter from registerSearchFlags.
func TestSearchCommandsHaveAllSharedFilters(t *testing.T) {
	sharedFilters := []string{
		// Required filters
		"--geo-id", "--permit-from", "--permit-to",
		// Permit filters
		"--tags", "--query", "--status",
		"--min-approval-duration", "--min-construction-duration",
		"--min-inspection-pr", "--min-job-value", "--min-fees",
		// Property filters
		"--property-type", "--property-min-market-value",
		"--property-min-building-area", "--property-min-lot-size",
		"--property-min-story-count", "--property-min-unit-count",
		// Contractor filters
		"--contractor-classification", "--contractor-name",
		"--contractor-website", "--contractor-min-total-job-value",
		"--contractor-min-total-permits-count",
		"--contractor-min-inspection-pr", "--contractor-license",
		// Response options
		"--include-count",
	}

	for _, cmd := range []string{"permits search", "contractors search"} {
		s := LookupSchema(cmd)
		if s == nil {
			t.Fatalf("schema for %q not found", cmd)
		}
		for _, filter := range sharedFilters {
			if _, ok := s.Filters[filter]; !ok {
				t.Errorf("schema for %q missing filter %s", cmd, filter)
			}
		}
	}
}

// TestPermitsSearchHasContractorFilter verifies permits search includes
// the --has-contractor flag unique to that command.
func TestPermitsSearchHasContractorFilter(t *testing.T) {
	s := LookupSchema("permits search")
	if s == nil {
		t.Fatal("permits search schema not found")
	}
	if _, ok := s.Filters["--has-contractor"]; !ok {
		t.Error("permits search missing --has-contractor filter")
	}
}

// TestContractorsSearchHasNoTalliesFilter verifies contractors search
// includes the --no-tallies flag unique to that command.
func TestContractorsSearchHasNoTalliesFilter(t *testing.T) {
	s := LookupSchema("contractors search")
	if s == nil {
		t.Fatal("contractors search schema not found")
	}
	if _, ok := s.Filters["--no-tallies"]; !ok {
		t.Error("contractors search missing --no-tallies filter")
	}
}

// TestMetricsCommandsHaveTagFilter verifies metrics commands include
// the --tag filter.
func TestMetricsCommandsHaveTagFilter(t *testing.T) {
	metricsCmds := []string{
		"cities metrics current",
		"cities metrics monthly",
		"counties metrics current",
		"addresses metrics current",
		"contractors metrics",
	}
	for _, cmd := range metricsCmds {
		s := LookupSchema(cmd)
		if s == nil {
			t.Fatalf("schema for %q not found", cmd)
		}
		if _, ok := s.Filters["--tag"]; !ok {
			t.Errorf("schema for %q missing --tag filter", cmd)
		}
	}
}

// TestAddressesMetricsLacksPropertyType verifies addresses metrics commands
// do not include --property-type (unlike cities/counties/jurisdictions).
func TestAddressesMetricsLacksPropertyType(t *testing.T) {
	for _, cmd := range []string{"addresses metrics current", "addresses metrics monthly"} {
		s := LookupSchema(cmd)
		if s == nil {
			t.Fatalf("schema for %q not found", cmd)
		}
		if _, ok := s.Filters["--property-type"]; ok {
			t.Errorf("schema for %q should not have --property-type filter", cmd)
		}
	}
}

// TestCitiesMetricsHasPropertyType verifies cities metrics commands include
// the --property-type filter.
func TestCitiesMetricsHasPropertyType(t *testing.T) {
	for _, cmd := range []string{"cities metrics current", "cities metrics monthly"} {
		s := LookupSchema(cmd)
		if s == nil {
			t.Fatalf("schema for %q not found", cmd)
		}
		if _, ok := s.Filters["--property-type"]; !ok {
			t.Errorf("schema for %q missing --property-type filter", cmd)
		}
	}
}

// TestMonthlyMetricsHaveDateFilters verifies monthly metrics commands
// include --metric-from and --metric-to filters.
func TestMonthlyMetricsHaveDateFilters(t *testing.T) {
	monthlyCmds := []string{
		"cities metrics monthly",
		"counties metrics monthly",
		"jurisdictions metrics monthly",
		"addresses metrics monthly",
	}
	for _, cmd := range monthlyCmds {
		s := LookupSchema(cmd)
		if s == nil {
			t.Fatalf("schema for %q not found", cmd)
		}
		if _, ok := s.Filters["--metric-from"]; !ok {
			t.Errorf("schema for %q missing --metric-from filter", cmd)
		}
		if _, ok := s.Filters["--metric-to"]; !ok {
			t.Errorf("schema for %q missing --metric-to filter", cmd)
		}
	}
}

// TestCurrentMetricsLackDateFilters verifies current metrics commands
// do not include --metric-from or --metric-to.
func TestCurrentMetricsLackDateFilters(t *testing.T) {
	currentCmds := []string{
		"cities metrics current",
		"counties metrics current",
		"jurisdictions metrics current",
		"addresses metrics current",
	}
	for _, cmd := range currentCmds {
		s := LookupSchema(cmd)
		if s == nil {
			t.Fatalf("schema for %q not found", cmd)
		}
		if _, ok := s.Filters["--metric-from"]; ok {
			t.Errorf("schema for %q should not have --metric-from filter", cmd)
		}
		if _, ok := s.Filters["--metric-to"]; ok {
			t.Errorf("schema for %q should not have --metric-to filter", cmd)
		}
	}
}

// TestEnrichedFieldsHaveUnits verifies that fields with unit overrides
// have their unit populated in the generated schema.
func TestEnrichedFieldsHaveUnits(t *testing.T) {
	s := LookupSchema("permits search")
	if s == nil {
		t.Fatal("permits search schema not found")
	}

	cases := []struct {
		field string
		unit  string
	}{
		{"job_value", "cents"},
		{"fees", "cents"},
		{"approval_duration", "days"},
		{"construction_duration", "days"},
		{"inspection_pass_rate", "percent"},
	}
	for _, tc := range cases {
		f, ok := s.ResponseFields[tc.field]
		if !ok {
			t.Errorf("permits search missing field %q", tc.field)
			continue
		}
		if f.Unit != tc.unit {
			t.Errorf("permits search field %q: expected unit %q, got %q", tc.field, tc.unit, f.Unit)
		}
	}
}

// TestEnrichedFieldsHaveRanges verifies that fields with range overrides
// have their range populated in the generated schema.
func TestEnrichedFieldsHaveRanges(t *testing.T) {
	s := LookupSchema("permits search")
	if s == nil {
		t.Fatal("permits search schema not found")
	}

	f, ok := s.ResponseFields["inspection_pass_rate"]
	if !ok {
		t.Fatal("permits search missing inspection_pass_rate field")
	}
	if f.Range != "0-100" {
		t.Errorf("expected range '0-100', got %q", f.Range)
	}
}

// TestGeoSearchCommandsHaveQueryFilter verifies geographic search commands
// include the --query filter.
func TestGeoSearchCommandsHaveQueryFilter(t *testing.T) {
	geoCmds := []string{
		"cities search",
		"counties search",
		"jurisdictions search",
		"addresses search",
		"zipcodes search",
		"states search",
	}
	for _, cmd := range geoCmds {
		s := LookupSchema(cmd)
		if s == nil {
			t.Fatalf("schema for %q not found", cmd)
		}
		if _, ok := s.Filters["--query"]; !ok {
			t.Errorf("schema for %q missing --query filter", cmd)
		}
	}
}

// TestMergeFieldEnumOverride verifies enum override is applied.
func TestMergeFieldEnumOverride(t *testing.T) {
	base := SchemaField{Type: "string"}
	override := OverrideField{Enum: "active, final, in_review"}
	result := MergeField(base, override)
	if result.Enum != "active, final, in_review" {
		t.Errorf("expected enum override, got %q", result.Enum)
	}
}

// TestMalformedYAMLReturnsError verifies YAML parse errors surface
// through the OverrideCommand unmarshaling path.
func TestMalformedYAMLReturnsError(t *testing.T) {
	// yaml.v3 accepts many inputs as valid YAML scalars. This input
	// has structural errors that the parser rejects.
	badYAML := []byte("key: [unclosed bracket")
	var overrides map[string]OverrideCommand
	err := yaml.Unmarshal(badYAML, &overrides)
	if err == nil {
		t.Error("expected parse error for malformed YAML")
	}
}
