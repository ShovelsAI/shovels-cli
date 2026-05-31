package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/shovels-ai/shovels-cli/internal/config"
	"github.com/spf13/cobra"
)

// newTestCoverageCmd builds a coverage command wired to runCoverage(geoType)
// with the flags the factory reads (global + coverage-specific) registered
// locally so it can be exercised in isolation.
func newTestCoverageCmd(geoType string) (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{
		Use:           "coverage GEO_ID",
		Args:          exactArgsUnlessSchema(1),
		RunE:          runCoverage(geoType),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	registerCoverageFlags(cmd)
	cmd.Flags().Bool("dry-run", false, "")
	cmd.Flags().String("timeout", "30s", "")
	cmd.Flags().Bool("no-retry", false, "")
	cmd.Flags().String("limit", "50", "")
	cmd.Flags().Int("max-records", 10000, "")

	var out bytes.Buffer
	cmd.SetOut(&out)
	return cmd, &out
}

// runTestCoverage executes the test coverage command with the given args and
// returns the captured stdout buffer and the RunE error.
func runTestCoverage(t *testing.T, geoType string, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	cmd, out := newTestCoverageCmd(geoType)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out, err
}

func TestCoverageMissingBothDatesIsValidationError(t *testing.T) {
	_, err := runTestCoverage(t, "zipcode", "92024")
	ee, ok := err.(*exitError)
	if !ok {
		t.Fatalf("expected *exitError, got %T (%v)", err, err)
	}
	if ee.code != 1 {
		t.Errorf("expected exit code 1, got %d", ee.code)
	}
}

func TestCoverageMissingArgIsCobraError(t *testing.T) {
	cmd, _ := newTestCoverageCmd("zipcode")
	cmd.SetArgs([]string{"--coverage-from", "2024-01-01", "--coverage-to", "2024-12-31"})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected an error for missing GEO_ID arg")
	}
}

func TestCoverageBadFromDateIsValidationError(t *testing.T) {
	_, err := runTestCoverage(t, "zipcode", "92024", "--coverage-from", "2024/01/01", "--coverage-to", "2024-12-31")
	ee, ok := err.(*exitError)
	if !ok {
		t.Fatalf("expected *exitError, got %T (%v)", err, err)
	}
	if ee.code != 1 {
		t.Errorf("expected exit code 1, got %d", ee.code)
	}
}

func TestCoverageBadToDateIsValidationError(t *testing.T) {
	_, err := runTestCoverage(t, "zipcode", "92024", "--coverage-from", "2024-01-01", "--coverage-to", "2024-13-40x")
	ee, ok := err.(*exitError)
	if !ok {
		t.Fatalf("expected *exitError, got %T (%v)", err, err)
	}
	if ee.code != 1 {
		t.Errorf("expected exit code 1, got %d", ee.code)
	}
}

func TestCoverageDryRunQueryConstruction(t *testing.T) {
	prev := resolvedConfig
	resolvedConfig = config.Config{BaseURL: "https://api.shovels.ai/v2"}
	defer func() { resolvedConfig = prev }()

	out, err := runTestCoverage(t, "state", "CA", "--coverage-from", "2024-01-01", "--coverage-to", "2024-12-31", "--dry-run")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var dr struct {
		Method string         `json:"method"`
		URL    string         `json:"url"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal(out.Bytes(), &dr); err != nil {
		t.Fatalf("dry-run output not valid JSON: %v\n%s", err, out.String())
	}
	if dr.Method != "GET" {
		t.Errorf("expected GET, got %q", dr.Method)
	}
	if dr.URL != "https://api.shovels.ai/v2/meta/coverage" {
		t.Errorf("expected resolved /v2/meta/coverage URL, got %q", dr.URL)
	}
	if dr.Params["geo_type"] != "state" {
		t.Errorf("expected geo_type=state, got %v", dr.Params["geo_type"])
	}
	if dr.Params["geo_id"] != "CA" {
		t.Errorf("expected geo_id=CA, got %v", dr.Params["geo_id"])
	}
	if dr.Params["date_from"] != "2024-01-01" {
		t.Errorf("expected date_from=2024-01-01, got %v", dr.Params["date_from"])
	}
	if dr.Params["date_to"] != "2024-12-31" {
		t.Errorf("expected date_to=2024-12-31, got %v", dr.Params["date_to"])
	}
	if _, ok := dr.Params["size"]; ok {
		t.Errorf("coverage dry-run must not include size, got %v", dr.Params["size"])
	}
}

func TestCoverageEnvelopeMapsItemsToData(t *testing.T) {
	var gotQuery url.Values
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query()
		w.WriteHeader(200)
		w.Write([]byte(`{"items":[{"field":"fees","tier":"missing","fill_pct":0.02,"permits_total":12034}]}`))
	}))
	defer srv.Close()

	prev := resolvedConfig
	resolvedConfig = config.Config{BaseURL: srv.URL, APIKey: "sk-test"}
	defer func() { resolvedConfig = prev }()

	cmd, out := newTestCoverageCmd("city")
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"ABC", "--coverage-from", "2024-01-01", "--coverage-to", "2024-12-31"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotPath != "/meta/coverage" {
		t.Errorf("expected path /meta/coverage, got %q", gotPath)
	}
	if gotQuery.Get("geo_type") != "city" {
		t.Errorf("expected geo_type=city, got %q", gotQuery.Get("geo_type"))
	}

	var env struct {
		Data []map[string]any `json:"data"`
		Meta map[string]any   `json:"meta"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("output not valid envelope JSON: %v\n%s", err, out.String())
	}
	if len(env.Data) != 1 {
		t.Fatalf("expected 1 data item, got %d", len(env.Data))
	}
	if env.Data[0]["field"] != "fees" {
		t.Errorf("expected field=fees, got %v", env.Data[0]["field"])
	}
	if len(env.Meta) != 0 {
		t.Errorf("expected empty meta (credit-exempt), got %v", env.Meta)
	}
}

func TestCoverageMalformedBodyIsClientError(t *testing.T) {
	cases := map[string]string{
		"not-json":      `nope`,
		"missing-items": `{"foo":1}`,
		"items-null":    `{"items":null}`,
		"items-object":  `{"items":{}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
				w.Write([]byte(body))
			}))
			defer srv.Close()

			prev := resolvedConfig
			resolvedConfig = config.Config{BaseURL: srv.URL, APIKey: "sk-test"}
			defer func() { resolvedConfig = prev }()

			cmd, out := newTestCoverageCmd("zipcode")
			cmd.SetContext(context.Background())
			cmd.SetArgs([]string{"92024", "--coverage-from", "2024-01-01", "--coverage-to", "2024-12-31"})
			err := cmd.Execute()

			ee, ok := err.(*exitError)
			if !ok {
				t.Fatalf("expected *exitError, got %T (%v)", err, err)
			}
			if ee.code != 1 {
				t.Errorf("expected exit code 1, got %d", ee.code)
			}
			if out.Len() != 0 {
				t.Errorf("expected no stdout envelope on parse failure, got %q", out.String())
			}
		})
	}
}

func TestCoverageResponseUnmarshalEmptyArray(t *testing.T) {
	var c coverageResponse
	if err := json.Unmarshal([]byte(`{"items":[]}`), &c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !c.itemsPresent {
		t.Error("expected itemsPresent=true for empty array")
	}
	if len(c.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(c.Items))
	}
}

func TestCoverageResponseUnmarshalMissingKey(t *testing.T) {
	var c coverageResponse
	if err := json.Unmarshal([]byte(`{"other":1}`), &c); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.itemsPresent {
		t.Error("expected itemsPresent=false when items key absent")
	}
}

func TestCoverageResponseUnmarshalNonArrayFails(t *testing.T) {
	var c coverageResponse
	if err := json.Unmarshal([]byte(`{"items":"x"}`), &c); err == nil {
		t.Error("expected error when items is not an array")
	}
}

func TestCoverageCommandsRequireAuth(t *testing.T) {
	cmds := []*cobra.Command{
		citiesCoverageCmd, countiesCoverageCmd, jurisdictionsCoverageCmd,
		statesCoverageCmd, zipcodesCoverageCmd,
	}
	for _, c := range cmds {
		if c.Annotations[AnnotationRequiresAuth] != "true" {
			t.Errorf("%s coverage command must be annotated requires_auth", c.Parent().Name())
		}
	}
}

func TestCoverageCommandsRegistered(t *testing.T) {
	parents := map[string]*cobra.Command{
		"cities":        citiesCmd,
		"counties":      countiesCmd,
		"jurisdictions": jurisdictionsCmd,
		"states":        statesCmd,
		"zipcodes":      zipcodesCmd,
	}
	for name, parent := range parents {
		found := false
		for _, sub := range parent.Commands() {
			if sub.Name() == "coverage" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s should have a coverage subcommand", name)
		}
	}
}
