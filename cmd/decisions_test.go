package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// --- isZipGeoID (ZIP rejection) ---

func TestIsZipGeoID_BareFiveDigit(t *testing.T) {
	for _, z := range []string{"92024", "90210", "78701", "00000"} {
		if !isZipGeoID(z) {
			t.Errorf("expected %q to be rejected as a ZIP geo_id", z)
		}
	}
}

func TestIsZipGeoID_ZipPlusFour(t *testing.T) {
	for _, z := range []string{"92024-1234", "90210-0001"} {
		if !isZipGeoID(z) {
			t.Errorf("expected %q (ZIP+4) to be rejected", z)
		}
	}
}

func TestIsZipGeoID_PrefixedFormats(t *testing.T) {
	for _, z := range []string{
		"ZIP_90210", "CITY_LOS_ANGELES_CA", "COUNTY_LOS_ANGELES_CA",
		"STATE_CA", "ADDR_123",
		"zip_90210", "city_miami", // case-insensitive prefix
	} {
		if !isZipGeoID(z) {
			t.Errorf("expected prefixed geo_id %q to be rejected", z)
		}
	}
}

func TestIsZipGeoID_OpaqueIDsAccepted(t *testing.T) {
	for _, id := range []string{"a4xysKbZwqg", "xZ9q", "abc123def"} {
		if isZipGeoID(id) {
			t.Errorf("expected opaque geo_id %q to pass through (not a ZIP)", id)
		}
	}
}

func TestIsZipGeoID_StateCodesAccepted(t *testing.T) {
	for _, st := range []string{"CA", "ca", "TX", "ny", "Fl"} {
		if isZipGeoID(st) {
			t.Errorf("expected state code %q to pass through (not a ZIP)", st)
		}
	}
}

func TestIsZipGeoID_SixDigitsNotZip(t *testing.T) {
	// Only exactly 5 bare digits are treated as ZIP.
	if isZipGeoID("123456") {
		t.Error("expected 6-digit value to not be treated as a bare ZIP")
	}
	if isZipGeoID("1234") {
		t.Error("expected 4-digit value to not be treated as a bare ZIP")
	}
}

// --- buildDecisionsSearchQuery (param mapping) ---

func TestBuildDecisionsSearchQuery_RequiredParams(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
	)

	q := buildDecisionsSearchQuery(cmd)

	if got := q.Get("geo_id"); got != "CA" {
		t.Errorf("expected geo_id=CA, got %q", got)
	}
	if got := q.Get("decision_from"); got != "2024-01-01" {
		t.Errorf("expected decision_from=2024-01-01, got %q", got)
	}
	if got := q.Get("decision_to"); got != "2024-12-31" {
		t.Errorf("expected decision_to=2024-12-31, got %q", got)
	}
}

func TestBuildDecisionsSearchQuery_OptionalFiltersMapped(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--asset-class", "Residential",
		"--category", "Rezoning",
		"--subcategory", "X",
		"--property-type", "Y",
		"--min-project-value", "100000",
		"--max-project-value", "5000000",
		"--query", "downtown",
	)

	q := buildDecisionsSearchQuery(cmd)

	checks := map[string]string{
		"asset_class":       "Residential",
		"category":          "Rezoning",
		"subcategory":       "X",
		"property_type":     "Y",
		"min_project_value": "100000",
		"max_project_value": "5000000",
		"decision_q":        "downtown",
	}
	for param, want := range checks {
		if got := q.Get(param); got != want {
			t.Errorf("expected %s=%q, got %q", param, want, got)
		}
	}
}

func TestBuildDecisionsSearchQuery_RepeatableFlag(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--asset-class", "A",
		"--asset-class", "B",
	)

	q := buildDecisionsSearchQuery(cmd)
	vals := q["asset_class"]
	if len(vals) != 2 || vals[0] != "A" || vals[1] != "B" {
		t.Errorf("expected asset_class=[A B], got %v", vals)
	}
}

func TestBuildDecisionsSearchQuery_CommaSeparatedFlag(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--category", "Rezoning,Variance",
	)

	q := buildDecisionsSearchQuery(cmd)
	vals := q["category"]
	if len(vals) != 2 || vals[0] != "Rezoning" || vals[1] != "Variance" {
		t.Errorf("expected category=[Rezoning Variance], got %v", vals)
	}
}

func TestBuildDecisionsSearchQuery_IncludeCount(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--include-count",
	)

	q := buildDecisionsSearchQuery(cmd)
	if got := q.Get("include_count"); got != "true" {
		t.Errorf("expected include_count=true, got %q", got)
	}
}

func TestBuildDecisionsSearchQuery_OmittedOptionalsAbsent(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
	)

	q := buildDecisionsSearchQuery(cmd)
	for _, param := range []string{
		"asset_class", "category", "subcategory", "property_type",
		"min_project_value", "max_project_value", "decision_q", "include_count",
	} {
		if _, ok := q[param]; ok {
			t.Errorf("expected %s to be absent when flag omitted, got %v", param, q[param])
		}
	}
}

func TestBuildDecisionsSearchQuery_MinProjectValueZeroIncluded(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--min-project-value", "0",
	)

	q := buildDecisionsSearchQuery(cmd)
	if got := q.Get("min_project_value"); got != "0" {
		t.Errorf("expected min_project_value=0 when explicitly set, got %q", got)
	}
}

// --- validateDecisionsSearchFlags ---

func TestValidateDecisionsSearchFlags_MissingAll(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t)
	err := validateDecisionsSearchFlags(cmd)
	if err == nil {
		t.Fatal("expected validation error for missing required flags")
	}
}

func TestValidateDecisionsSearchFlags_QueryExactly100RunesAccepted(t *testing.T) {
	// 100 multi-byte runes (each 'é' is 2 bytes) — rune count is the limit.
	query := strings.Repeat("é", 100)
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--query", query,
	)
	if err := validateDecisionsSearchFlags(cmd); err != nil {
		t.Fatalf("expected 100-rune query to be accepted, got error: %v", err)
	}
}

func TestValidateDecisionsSearchFlags_Query101RunesRejected(t *testing.T) {
	query := strings.Repeat("a", 101)
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--query", query,
	)
	if err := validateDecisionsSearchFlags(cmd); err == nil {
		t.Fatal("expected 101-rune query to be rejected")
	}
}

func TestValidateDecisionsSearchFlags_MultiByteWithinRuneLimit(t *testing.T) {
	// 60 'é' runes = 120 bytes but only 60 runes — must be accepted since
	// the limit is rune-based, not byte-based.
	query := strings.Repeat("é", 60)
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--query", query,
	)
	if err := validateDecisionsSearchFlags(cmd); err != nil {
		t.Fatalf("expected 60-rune (120-byte) query to be accepted, got error: %v", err)
	}
}

func TestValidateDecisionsSearchFlags_NegativeMinProjectValueRejected(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--min-project-value", "-1",
	)
	if err := validateDecisionsSearchFlags(cmd); err == nil {
		t.Fatal("expected negative --min-project-value to be rejected")
	}
}

func TestValidateDecisionsSearchFlags_NegativeMaxProjectValueRejected(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--max-project-value", "-5",
	)
	if err := validateDecisionsSearchFlags(cmd); err == nil {
		t.Fatal("expected negative --max-project-value to be rejected")
	}
}

func TestValidateDecisionsSearchFlags_ZeroProjectValuesAccepted(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
		"--min-project-value", "0",
		"--max-project-value", "0",
	)
	if err := validateDecisionsSearchFlags(cmd); err != nil {
		t.Fatalf("expected zero project values to be accepted, got error: %v", err)
	}
}

func TestValidateDecisionsSearchFlags_SingleDayRangeAccepted(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-06-12",
		"--decision-to", "2024-06-12",
	)
	if err := validateDecisionsSearchFlags(cmd); err != nil {
		t.Fatalf("expected single-day range to be accepted, got error: %v", err)
	}
}

func TestValidateDecisionsSearchFlags_BadDateFormatRejected(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024/01/01",
		"--decision-to", "2024-12-31",
	)
	if err := validateDecisionsSearchFlags(cmd); err == nil {
		t.Fatal("expected bad date format to be rejected")
	}
}

func TestValidateDecisionsSearchFlags_InvalidCalendarDateLeftToAPI(t *testing.T) {
	// 2024-99-99 matches YYYY-MM-DD format-wise; calendar validity is the
	// API's job, so local validation must pass.
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "CA",
		"--decision-from", "2024-99-99",
		"--decision-to", "2024-12-31",
	)
	if err := validateDecisionsSearchFlags(cmd); err != nil {
		t.Fatalf("expected format-valid-but-invalid calendar date to pass local validation, got: %v", err)
	}
}

func TestValidateDecisionsSearchFlags_ZipRejected(t *testing.T) {
	cmd := newDecisionsSearchTestCmd(t,
		"--geo-id", "92024",
		"--decision-from", "2024-01-01",
		"--decision-to", "2024-12-31",
	)
	if err := validateDecisionsSearchFlags(cmd); err == nil {
		t.Fatal("expected bare ZIP geo_id to be rejected")
	}
}

// newDecisionsSearchTestCmd builds a fresh decisions search command with its
// flags registered and the given args parsed, isolated from the global tree.
func newDecisionsSearchTestCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "search"}
	registerDecisionsSearchFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("failed to parse flags: %v", err)
	}
	return cmd
}
