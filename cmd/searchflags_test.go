package cmd

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestIsValidPropertyType_OriginalThreeValues(t *testing.T) {
	for _, pt := range []string{"residential", "commercial", "industrial"} {
		if !isValidPropertyType(pt) {
			t.Errorf("expected %q to be a valid property type", pt)
		}
	}
}

func TestIsValidPropertyType_NewValues(t *testing.T) {
	for _, pt := range []string{"agricultural", "vacant land", "exempt", "miscellaneous", "office", "recreational"} {
		if !isValidPropertyType(pt) {
			t.Errorf("expected %q to be a valid property type", pt)
		}
	}
}

func TestIsValidPropertyType_VacantLandWithSpace(t *testing.T) {
	if !isValidPropertyType("vacant land") {
		t.Error("expected \"vacant land\" (with space) to be a valid property type")
	}
}

func TestIsValidPropertyType_InvalidValue(t *testing.T) {
	if isValidPropertyType("bogus") {
		t.Error("expected \"bogus\" to be rejected as invalid property type")
	}
}

func TestIsValidPropertyType_CaseSensitive(t *testing.T) {
	if isValidPropertyType("Residential") {
		t.Error("expected \"Residential\" (capitalized) to be rejected — validation is case-sensitive")
	}
}

func TestValidPropertyTypes_ContainsAllNine(t *testing.T) {
	if len(validPropertyTypes) != 9 {
		t.Errorf("expected 9 valid property types, got %d", len(validPropertyTypes))
	}
}

// --- Classification validation (unit) ---

func TestIsValidClassification_AllThirteenValues(t *testing.T) {
	all := []string{
		"concrete_and_paving", "demolition_and_excavation", "electrical",
		"fencing_and_glazing", "framing_and_carpentry", "general_building_contractor",
		"general_engineering_contractor", "hvac", "landscaping_and_outdoor_work",
		"other", "plumbing", "roofing", "specialty_trades",
	}
	for _, c := range all {
		if !isValidClassification(c) {
			t.Errorf("expected %q to be a valid classification", c)
		}
	}
}

func TestIsValidClassification_ExclusionPrefix(t *testing.T) {
	if !isValidClassification("-electrical") {
		t.Error("expected \"-electrical\" (exclusion prefix) to be valid after stripping dash")
	}
}

func TestIsValidClassification_InvalidValue(t *testing.T) {
	if isValidClassification("bogus") {
		t.Error("expected \"bogus\" to be rejected as invalid classification")
	}
}

func TestIsValidClassification_ExactMatch(t *testing.T) {
	if isValidClassification("general_building") {
		t.Error("expected \"general_building\" to be rejected — must match full enum value \"general_building_contractor\"")
	}
}

func TestValidClassifications_ContainsAllThirteen(t *testing.T) {
	if len(validClassifications) != 13 {
		t.Errorf("expected 13 valid classifications, got %d", len(validClassifications))
	}
}

// newSearchTestCmd builds a command carrying the shared search flags, so the
// query builder and validator can be exercised without a network round trip.
func newSearchTestCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "search"}
	registerSearchFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}
	return cmd
}

// /permits/search and /contractors/search accept property_type as repeated
// keys, so a union of types is expressible. Registering the flag as a scalar
// discarded every value but the last, turning "residential or commercial" into
// "commercial" — a narrower answer that still looks plausible.
func TestBuildSearchQuery_RepeatedPropertyTypeKeepsEveryValue(t *testing.T) {
	cmd := newSearchTestCmd(t,
		"--geo-id", "CA",
		"--property-type", "residential",
		"--property-type", "commercial",
	)

	got := buildSearchQuery(cmd)["property_type"]

	want := []string{"residential", "commercial"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("expected property_type=%v, got %v", want, got)
	}
}

// The comma form has to reach the wire as separate values too: the API rejects
// a comma-joined value rather than splitting it.
func TestBuildSearchQuery_CommaSeparatedPropertyTypeSplits(t *testing.T) {
	cmd := newSearchTestCmd(t, "--geo-id", "CA", "--property-type", "residential,commercial")

	got := buildSearchQuery(cmd)["property_type"]

	want := []string{"residential", "commercial"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("expected property_type=%v, got %v", want, got)
	}
}

// "vacant land" carries a space, so CSV parsing has to leave it intact when it
// appears alongside another value.
func TestBuildSearchQuery_PropertyTypeWithSpaceSurvivesSplitting(t *testing.T) {
	cmd := newSearchTestCmd(t, "--geo-id", "CA", "--property-type", "vacant land,office")

	got := buildSearchQuery(cmd)["property_type"]

	want := []string{"vacant land", "office"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("expected property_type=%v, got %v", want, got)
	}
}

// Validation runs per element. Checking the joined value would reject a
// combination the API accepts.
func TestValidateSearchFlags_EveryPropertyTypeElementAccepted(t *testing.T) {
	cmd := newSearchTestCmd(t,
		"--geo-id", "CA",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--property-type", "residential,vacant land",
	)

	if err := validateSearchFlags(cmd); err != nil {
		t.Errorf("expected a valid combination to pass, got %v", err)
	}
}

// A typo is caught in any position. The trailing case alone would pass against
// a whole-string check, since "residential,bogus" is not in the vocabulary
// either; the leading case is what requires per-element validation.
func TestValidateSearchFlags_InvalidPropertyTypeElementRejected(t *testing.T) {
	for _, value := range []string{"residential,bogus", "bogus,residential"} {
		cmd := newSearchTestCmd(t,
			"--geo-id", "CA",
			"--permit-from", "2024-01-01",
			"--permit-to", "2024-12-31",
			"--property-type", value,
		)

		if err := validateSearchFlags(cmd); err == nil {
			t.Errorf("expected %q to be rejected", value)
		}
	}
}

// pflag drops an entirely empty occurrence, so nothing reaches validation and
// the parameter is omitted. The API treats an empty property_type exactly as
// absent, so the search is unfiltered either way — the same outcome the scalar
// flag produced by sending property_type= explicitly.
func TestBuildSearchQuery_EmptyPropertyTypeOccurrenceOmitsTheParam(t *testing.T) {
	cmd := newSearchTestCmd(t, "--geo-id", "CA", "--property-type=")

	if _, present := buildSearchQuery(cmd)["property_type"]; present {
		t.Error("expected property_type to be absent for an empty occurrence")
	}
}

// An empty occurrence alongside a real one leaves the real one standing. The
// scalar flag resolved this by last-value-wins, so the empty erased the filter
// and silently widened the search.
func TestBuildSearchQuery_EmptyPropertyTypeDoesNotEraseAValidOne(t *testing.T) {
	cmd := newSearchTestCmd(t, "--geo-id", "CA", "--property-type", "residential", "--property-type=")

	got := buildSearchQuery(cmd)["property_type"]

	if len(got) != 1 || got[0] != "residential" {
		t.Errorf("expected property_type=[residential], got %v", got)
	}
}

// A stray comma yields an empty element, which is a typo rather than a value.
func TestValidateSearchFlags_TrailingCommaInPropertyTypeRejected(t *testing.T) {
	cmd := newSearchTestCmd(t,
		"--geo-id", "CA",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--property-type", "residential,",
	)

	if err := validateSearchFlags(cmd); err == nil {
		t.Error("expected a trailing comma to be rejected")
	}
}
