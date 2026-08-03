package cmd

import (
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
