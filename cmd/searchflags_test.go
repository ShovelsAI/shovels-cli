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
