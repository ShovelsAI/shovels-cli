package contract

import (
	"slices"
	"testing"
)

// --- Happy paths ---

func TestCursorPaginatedRecordHonorsAllFiveFlags(t *testing.T) {
	record, ok := Lookup("permits search")
	if !ok {
		t.Fatal("permits search has no contract record")
	}

	if !slices.Equal(record.Honored, APIOnlyFlags()) {
		t.Errorf("expected %v, got %v", APIOnlyFlags(), record.Honored)
	}
}

func TestCitiesSearchIsClassifiedServerCappedWithACap(t *testing.T) {
	record, ok := Lookup("cities search")
	if !ok {
		t.Fatal("cities search has no contract record")
	}

	if record.Mode != ModeServerCapped || record.Cap <= 0 {
		t.Errorf("expected mode %q with a positive cap, got mode %q cap %d",
			ModeServerCapped, record.Mode, record.Cap)
	}
}

func TestAllReturnsACopyOfTheTable(t *testing.T) {
	records := All()
	delete(records, "permits search")

	if _, ok := Lookup("permits search"); !ok {
		t.Error("mutating the map All returned changed the table")
	}
}

// --- Edge cases ---

func TestConfigSetHonorsDryRunAlone(t *testing.T) {
	record, ok := Lookup("config set")
	if !ok {
		t.Fatal("config set has no contract record")
	}

	if !slices.Equal(record.Honored, []string{FlagDryRun}) {
		t.Errorf("expected [%s] alone, got %v", FlagDryRun, record.Honored)
	}
}

func TestTransportOnlyRecordHonorsNeitherPaginationFlag(t *testing.T) {
	record, ok := Lookup("contractors metrics")
	if !ok {
		t.Fatal("contractors metrics has no contract record")
	}

	if record.Honors(FlagLimit) || record.Honors(FlagMaxRecords) {
		t.Errorf("a command issuing one request bounds nothing, got %v", record.Honored)
	}
}

func TestVersionHonorsNothing(t *testing.T) {
	record, ok := Lookup("version")
	if !ok {
		t.Fatal("version has no contract record")
	}

	if len(record.Honored) != 0 {
		t.Errorf("version builds its client with a fixed timeout, got %v", record.Honored)
	}
}

// --- Error conditions ---

func TestValidateRejectsServerCappedWithoutACap(t *testing.T) {
	record := Record{Honored: APIOnlyFlags(), Mode: ModeServerCapped}

	err := record.Validate()

	if err == nil {
		t.Errorf("mode %q with no cap has nothing to disclose and must not validate", ModeServerCapped)
	}
}

func TestValidateRejectsACapOnACursorPaginatedRecord(t *testing.T) {
	record := Record{Honored: APIOnlyFlags(), Mode: ModeCursor, Cap: 15}

	err := record.Validate()

	if err == nil {
		t.Error("a cursor-paginated command has no cap and must not validate with one")
	}
}

func TestValidateRejectsAnUnknownFlag(t *testing.T) {
	record := Record{Honored: []string{"base-url"}, Mode: ModeNone}

	err := record.Validate()

	if err == nil {
		t.Error("only the API-only flags are scoped per command, so --base-url must not validate")
	}
}

func TestValidateRejectsARepeatedFlag(t *testing.T) {
	record := Record{Honored: []string{FlagDryRun, FlagDryRun}, Mode: ModeNone}

	err := record.Validate()

	if err == nil {
		t.Error("a flag named twice must not validate")
	}
}

func TestValidateRejectsAnUnknownMode(t *testing.T) {
	record := Record{Mode: Mode("offset")}

	err := record.Validate()

	if err == nil {
		t.Error("an unknown pagination mode must not validate")
	}
}

func TestValidateRejectsLimitOnAnUnpaginatedRecord(t *testing.T) {
	record := Record{Honored: []string{FlagLimit, FlagMaxRecords}, Mode: ModeNone}

	err := record.Validate()

	if err == nil {
		t.Errorf("mode %q collects one response, so honoring --limit must not validate", ModeNone)
	}
}

// --- Boundary conditions ---

func TestRegisterPanicsOnADuplicatePath(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("registering a second record for one command must panic")
		}
	}()

	Register("version", Record{Mode: ModeNone})
}
