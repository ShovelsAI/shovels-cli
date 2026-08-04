package cmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// --- buildPropertiesSearchQuery (param mapping) ---

func TestBuildPropertiesSearchQuery_GeoIDOnly(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t, "--geo-id", "CA")

	q := buildPropertiesSearchQuery(cmd)

	if got := q.Get("geo_id"); got != "CA" {
		t.Errorf("expected geo_id=CA, got %q", got)
	}
	if _, ok := q["legal_owner"]; ok {
		t.Errorf("expected legal_owner absent, got %v", q["legal_owner"])
	}
}

func TestBuildPropertiesSearchQuery_LegalOwnerOnlyOmitsGeoID(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t, "--legal-owner", "INVITATION HOMES")

	q := buildPropertiesSearchQuery(cmd)

	if _, ok := q["geo_id"]; ok {
		t.Errorf("expected geo_id absent for a nationwide owner search, got %v", q["geo_id"])
	}
	if vals := q["legal_owner"]; len(vals) != 1 || vals[0] != "INVITATION HOMES" {
		t.Errorf("expected legal_owner=[INVITATION HOMES], got %v", vals)
	}
}

func TestBuildPropertiesSearchQuery_LegalOwnerCommasNotSplit(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t,
		"--legal-owner", "SMITH, JOHN",
		"--legal-owner", "ACME LLC",
	)

	q := buildPropertiesSearchQuery(cmd)

	vals := q["legal_owner"]
	if len(vals) != 2 || vals[0] != "SMITH, JOHN" || vals[1] != "ACME LLC" {
		t.Errorf("expected two owner params with commas preserved, got %v", vals)
	}
}

func TestBuildPropertiesSearchQuery_GeoIDAndLegalOwnerBothSent(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t,
		"--geo-id", "92024",
		"--legal-owner", "ACME LLC",
	)

	q := buildPropertiesSearchQuery(cmd)

	if got := q.Get("geo_id"); got != "92024" {
		t.Errorf("expected geo_id=92024, got %q", got)
	}
	if got := q.Get("legal_owner"); got != "ACME LLC" {
		t.Errorf("expected legal_owner=ACME LLC, got %q", got)
	}
}

func TestBuildPropertiesSearchQuery_PermitFiltersMapped(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t,
		"--geo-id", "CA",
		"--permit-tags", "solar,-roofing",
		"--permit-status", "final,active",
		"--permit-from", "2024-01-01",
		"--permit-tags-unfinaled", "solar",
	)

	q := buildPropertiesSearchQuery(cmd)

	checks := map[string]string{
		"permit_tags":           "solar,-roofing",
		"permit_status":         "final,active",
		"permit_from":           "2024-01-01",
		"permit_tags_unfinaled": "solar",
	}
	for param, want := range checks {
		if got := q[param]; len(got) != 1 || got[0] != want {
			t.Errorf("expected %s=[%q], got %v", param, want, got)
		}
	}
}

func TestBuildPropertiesSearchQuery_IncludeCountMapsToIncludeTotalCount(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t, "--geo-id", "CA", "--include-count")

	q := buildPropertiesSearchQuery(cmd)

	if got := q.Get("include_total_count"); got != "true" {
		t.Errorf("expected include_total_count=true, got %q", got)
	}
	if _, ok := q["include_count"]; ok {
		t.Error("expected the permits-style include_count param to be absent")
	}
}

func TestBuildPropertiesSearchQuery_OmittedOptionalsAbsent(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t, "--geo-id", "CA")

	q := buildPropertiesSearchQuery(cmd)

	for _, param := range []string{
		"legal_owner", "permit_tags", "permit_status", "permit_from",
		"permit_tags_unfinaled", "include_total_count",
	} {
		if _, ok := q[param]; ok {
			t.Errorf("expected %s to be absent when its flag is omitted, got %v", param, q[param])
		}
	}
}

func TestBuildPropertiesSearchQuery_NoPermitToParam(t *testing.T) {
	// The endpoint documents permit_to purely as a rejection, so the CLI must
	// never emit it.
	cmd := newPropertiesSearchTestCmd(t, "--geo-id", "CA", "--permit-from", "2024-01-01")

	q := buildPropertiesSearchQuery(cmd)

	if _, ok := q["permit_to"]; ok {
		t.Errorf("expected permit_to never to be sent, got %v", q["permit_to"])
	}
}

// --- property attribute filters (flags to params) ---

func TestBuildPropertiesSearchQuery_PropertyTypeRepeatsParam(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t,
		"--geo-id", "CA",
		"--property-type", "residential,commercial",
		"--property-type", "vacant land",
	)

	q := buildPropertiesSearchQuery(cmd)

	vals := q["property_type"]
	want := []string{"residential", "commercial", "vacant land"}
	if len(vals) != len(want) {
		t.Fatalf("expected %d property_type params, got %v", len(want), vals)
	}
	for i, v := range want {
		if vals[i] != v {
			t.Errorf("property_type[%d]: expected %q, got %q", i, v, vals[i])
		}
	}
}

// propertyRangeCases spells out both bounds of every property attribute range
// filter and the API parameter each maps to. Nothing here is read back from
// the command's own tables, so a pair the command stops registering,
// validating, or sending fails a test instead of vanishing from both sides at
// once.
var propertyRangeCases = []struct {
	minFlag, minParam string
	maxFlag, maxParam string
}{
	{"property-min-market-value", "property_min_market_value", "property-max-market-value", "property_max_market_value"},
	{"property-min-lot-size", "property_min_lot_size", "property-max-lot-size", "property_max_lot_size"},
	{"property-min-building-area", "property_min_building_area", "property-max-building-area", "property_max_building_area"},
	{"property-min-unit-count", "property_min_unit_count", "property-max-unit-count", "property_max_unit_count"},
	{"property-min-year-built", "property_min_year_built", "property-max-year-built", "property_max_year_built"},
}

func TestBuildPropertiesSearchQuery_EveryRangeBoundMapped(t *testing.T) {
	// Every bound carries a distinct value, so a flag wired to a neighbouring
	// param surfaces as a value mismatch instead of passing silently.
	args := []string{"--geo-id", "CA"}
	want := map[string]string{}
	for i, rc := range propertyRangeCases {
		lower := fmt.Sprintf("%d", 100*(i+1))
		upper := fmt.Sprintf("%d", 100*(i+1)+50)
		args = append(args, "--"+rc.minFlag, lower, "--"+rc.maxFlag, upper)
		want[rc.minParam] = lower
		want[rc.maxParam] = upper
	}
	cmd := newPropertiesSearchTestCmd(t, args...)

	q := buildPropertiesSearchQuery(cmd)

	for param, value := range want {
		if got := q[param]; len(got) != 1 || got[0] != value {
			t.Errorf("expected %s=[%q], got %v", param, value, got)
		}
	}
}

func TestBuildPropertiesSearchQuery_NoStoryCountPair(t *testing.T) {
	// Permits search filters on story count; the Properties API has no such
	// parameter, so neither the flags nor the params may exist here.
	cmd := newPropertiesSearchTestCmd(t, "--geo-id", "CA")

	for _, flag := range []string{"property-min-story-count", "property-max-story-count"} {
		if cmd.Flags().Lookup(flag) != nil {
			t.Errorf("expected no --%s flag on properties search", flag)
		}
	}

	q := buildPropertiesSearchQuery(cmd)
	for _, param := range []string{"property_min_story_count", "property_max_story_count"} {
		if _, ok := q[param]; ok {
			t.Errorf("expected %s never to be sent, got %v", param, q[param])
		}
	}
}

func TestBuildPropertiesSearchQuery_UnsetAttributeFiltersAbsent(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t, "--geo-id", "CA")

	q := buildPropertiesSearchQuery(cmd)

	if _, ok := q["property_type"]; ok {
		t.Errorf("expected property_type absent when its flag is omitted, got %v", q["property_type"])
	}
	for _, rc := range propertyRangeCases {
		for _, param := range []string{rc.minParam, rc.maxParam} {
			if _, ok := q[param]; ok {
				t.Errorf("expected %s absent when its flag is omitted, got %v", param, q[param])
			}
		}
	}
}

func TestBuildPropertiesSearchQuery_ExplicitZeroBoundSent(t *testing.T) {
	// 0 is the flag's zero value, so only explicit-set detection distinguishes
	// "no filter" from "at least zero units".
	cmd := newPropertiesSearchTestCmd(t, "--geo-id", "CA", "--property-min-unit-count", "0")

	q := buildPropertiesSearchQuery(cmd)

	if got := q["property_min_unit_count"]; len(got) != 1 || got[0] != "0" {
		t.Errorf("expected property_min_unit_count=[0], got %v", got)
	}
}

func TestBuildPropertiesSearchQuery_EqualBoundsBothSent(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t,
		"--geo-id", "CA",
		"--property-min-year-built", "1985",
		"--property-max-year-built", "1985",
	)

	q := buildPropertiesSearchQuery(cmd)

	if got := q.Get("property_min_year_built"); got != "1985" {
		t.Errorf("expected property_min_year_built=1985, got %q", got)
	}
	if got := q.Get("property_max_year_built"); got != "1985" {
		t.Errorf("expected property_max_year_built=1985, got %q", got)
	}
}

func TestBuildPropertiesSearchQuery_UnknownPropertyTypeForwarded(t *testing.T) {
	// The enum is the API's to police; the CLI forwards so the server's 422
	// carries the authoritative value list.
	cmd := newPropertiesSearchTestCmd(t, "--geo-id", "CA", "--property-type", "castle")

	q := buildPropertiesSearchQuery(cmd)

	if got := q["property_type"]; len(got) != 1 || got[0] != "castle" {
		t.Errorf("expected the unknown type forwarded as property_type=[castle], got %v", got)
	}
}

// --- validatePropertiesSearchFlags ---

func TestValidatePropertiesSearchFlags_NoScopeRejected(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t)

	if err := validatePropertiesSearchFlags(cmd); err == nil {
		t.Fatal("expected a scope error when neither --geo-id nor --legal-owner is given")
	}
}

func TestValidatePropertiesSearchFlags_EmptyGeoIDAloneRejected(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t, "--geo-id", "")

	if err := validatePropertiesSearchFlags(cmd); err == nil {
		t.Fatal("expected an empty --geo-id to be treated as absent")
	}
}

func TestValidatePropertiesSearchFlags_OnlyEmptyOwnersRejected(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t, "--legal-owner", "")

	if err := validatePropertiesSearchFlags(cmd); err == nil {
		t.Fatal("expected an empty --legal-owner alone to fail the scope requirement")
	}
}

func TestValidatePropertiesSearchFlags_EmptyOwnerAmongValuesRejected(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t,
		"--legal-owner", "ACME LLC",
		"--legal-owner", "",
	)

	if err := validatePropertiesSearchFlags(cmd); err == nil {
		t.Fatal("expected an empty --legal-owner value to be rejected")
	}
}

func TestValidatePropertiesSearchFlags_GeoIDOnlyAccepted(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t, "--geo-id", "92024")

	if err := validatePropertiesSearchFlags(cmd); err != nil {
		t.Fatalf("expected --geo-id alone to satisfy the scope requirement, got %v", err)
	}
}

func TestValidatePropertiesSearchFlags_LegalOwnerOnlyAccepted(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t, "--legal-owner", "INVITATION HOMES")

	if err := validatePropertiesSearchFlags(cmd); err != nil {
		t.Fatalf("expected --legal-owner alone to satisfy the scope requirement, got %v", err)
	}
}

func TestValidatePropertiesSearchFlags_ExactlyTenOwnersAccepted(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t, ownerArgs(maxPropertyLegalOwners)...)

	if err := validatePropertiesSearchFlags(cmd); err != nil {
		t.Fatalf("expected %d owners to be accepted, got %v", maxPropertyLegalOwners, err)
	}
}

func TestValidatePropertiesSearchFlags_ElevenOwnersRejected(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t, ownerArgs(maxPropertyLegalOwners+1)...)

	if err := validatePropertiesSearchFlags(cmd); err == nil {
		t.Fatalf("expected more than %d owners to be rejected", maxPropertyLegalOwners)
	}
}

func TestValidatePropertiesSearchFlags_BadPermitFromShapeRejected(t *testing.T) {
	for _, from := range []string{"2024-1-1", "2024/01/01", "01-01-2024"} {
		cmd := newPropertiesSearchTestCmd(t, "--geo-id", "CA", "--permit-from", from)
		if err := validatePropertiesSearchFlags(cmd); err == nil {
			t.Errorf("expected --permit-from %q to be rejected", from)
		}
	}
}

func TestValidatePropertiesSearchFlags_InvalidCalendarDateLeftToAPI(t *testing.T) {
	// 2024-13-01 matches YYYY-MM-DD; calendar validity is the API's job.
	cmd := newPropertiesSearchTestCmd(t, "--geo-id", "CA", "--permit-from", "2024-13-01")

	if err := validatePropertiesSearchFlags(cmd); err != nil {
		t.Fatalf("expected a shape-valid date to pass local validation, got %v", err)
	}
}

func TestValidatePropertiesSearchFlags_ZipAndZipPlusFourAccepted(t *testing.T) {
	// Properties accepts ZIP scopes, diverging from decisions.
	for _, geoID := range []string{"92024", "92024-1234"} {
		cmd := newPropertiesSearchTestCmd(t, "--geo-id", geoID)
		if err := validatePropertiesSearchFlags(cmd); err != nil {
			t.Errorf("expected --geo-id %q to be accepted, got %v", geoID, err)
		}
	}
}

// --- validatePropertiesSearchFlags (property attribute ranges) ---

func TestValidatePropertiesSearchFlags_NegativeBoundRejected(t *testing.T) {
	for _, rc := range propertyRangeCases {
		for _, flag := range []string{rc.minFlag, rc.maxFlag} {
			cmd := newPropertiesSearchTestCmd(t, "--geo-id", "CA", "--"+flag, "-1")
			if err := validatePropertiesSearchFlags(cmd); err == nil {
				t.Errorf("expected --%s -1 to be rejected", flag)
			}
		}
	}
}

func TestValidatePropertiesSearchFlags_InvertedPairRejected(t *testing.T) {
	for _, rc := range propertyRangeCases {
		cmd := newPropertiesSearchTestCmd(t,
			"--geo-id", "CA",
			"--"+rc.minFlag, "20",
			"--"+rc.maxFlag, "10",
		)
		if err := validatePropertiesSearchFlags(cmd); err == nil {
			t.Errorf("expected --%s 20 with --%s 10 to be rejected", rc.minFlag, rc.maxFlag)
		}
	}
}

func TestValidatePropertiesSearchFlags_DoublyInvalidPairRejected(t *testing.T) {
	// -5 exceeds -10, so this pair is both negative and inverted. Which of the
	// two messages is emitted is pinned end-to-end; here only the rejection
	// matters.
	cmd := newPropertiesSearchTestCmd(t,
		"--geo-id", "CA",
		"--property-min-market-value", "-5",
		"--property-max-market-value", "-10",
	)

	if err := validatePropertiesSearchFlags(cmd); err == nil {
		t.Fatal("expected a pair that is both negative and inverted to be rejected")
	}
}

func TestValidatePropertiesSearchFlags_EqualBoundsAccepted(t *testing.T) {
	for _, rc := range propertyRangeCases {
		cmd := newPropertiesSearchTestCmd(t,
			"--geo-id", "CA",
			"--"+rc.minFlag, "12",
			"--"+rc.maxFlag, "12",
		)
		if err := validatePropertiesSearchFlags(cmd); err != nil {
			t.Errorf("expected --%s == --%s to be an exact-value match, got %v", rc.minFlag, rc.maxFlag, err)
		}
	}
}

func TestValidatePropertiesSearchFlags_LoneBoundNeverInverted(t *testing.T) {
	// An unset counterpart holds 0, which must not be read as an upper bound.
	for _, rc := range propertyRangeCases {
		cmd := newPropertiesSearchTestCmd(t, "--geo-id", "CA", "--"+rc.minFlag, "5000")
		if err := validatePropertiesSearchFlags(cmd); err != nil {
			t.Errorf("expected --%s alone to be accepted, got %v", rc.minFlag, err)
		}
	}
}

func TestValidatePropertiesSearchFlags_ZeroBoundsAccepted(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t,
		"--geo-id", "CA",
		"--property-min-unit-count", "0",
		"--property-max-unit-count", "0",
	)

	if err := validatePropertiesSearchFlags(cmd); err != nil {
		t.Fatalf("expected zero bounds to be accepted, got %v", err)
	}
}

func TestValidatePropertiesSearchFlags_UnknownPropertyTypeAccepted(t *testing.T) {
	cmd := newPropertiesSearchTestCmd(t, "--geo-id", "CA", "--property-type", "castle")

	if err := validatePropertiesSearchFlags(cmd); err != nil {
		t.Fatalf("expected an unknown property type to be left to the API, got %v", err)
	}
}

// --- helpers ---

// ownerArgs builds n distinct --legal-owner flag pairs.
func ownerArgs(n int) []string {
	args := make([]string, 0, n*2)
	for i := range n {
		args = append(args, "--legal-owner", "OWNER "+strings.Repeat("X", i+1))
	}
	return args
}

// newPropertiesSearchTestCmd builds a fresh properties search command with its
// flags registered and the given args parsed, isolated from the global tree.
func newPropertiesSearchTestCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "search"}
	registerPropertiesSearchFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}
	return cmd
}
