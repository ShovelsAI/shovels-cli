package cmd

import "testing"

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
