package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Metrics are scoped to one resolved geography, so a bare ZIP, a ZIP+4, and a
// state code are all wrong here. The API rejects every one of them, but with
// its shared geo_id validator message, which lists "5-digit ZIP" among the
// accepted formats and so names the exact format it just refused. These cases
// pin the local guard that answers before the request goes out.
func TestValidateMetricsGeoIDRejectsWrongScopeForms(t *testing.T) {
	cfg := newMetricsConfig("cities", "city", true)

	for _, geoID := range []string{
		"92024",      // bare ZIP
		"92024-1234", // ZIP+4
		"CA",         // 2-letter state code
		"ca",         // lowercase: /permits/search returns 200 for geo_id=ca, so
		"Ca",         // users learn this form and carry it here
	} {
		if err := validateMetricsGeoID(cfg, geoID); err == nil {
			t.Errorf("expected %q to be rejected as out of scope for city metrics", geoID)
		}
	}
}

// The guard must never reject on a prefix heuristic. Opaque geo_ids are
// 11-character base64url strings whose alphabet includes the underscore, so a
// server-issued ID can legitimately look like a fabricated prefixed format.
// Refusing one would be unrecoverable for the user — there is no bypass flag —
// and invisible server-side, because no request is made. Wrong-type opaque IDs
// must also pass through: a county ID is indistinguishable from a city ID here,
// and the API answers those with a message that already names the problem.
func TestValidateMetricsGeoIDNeverRejectsOpaqueIDs(t *testing.T) {
	cfg := newMetricsConfig("cities", "city", true)

	for _, geoID := range []string{
		"RMjg6rIIh2k", // a real city geo_id
		"xBw08qOlcdc", // a real county geo_id: wrong type, but not detectable here
		"w8aD_ZCQmSE", // real, underscore in the middle
		"oT_pAw3pfKY", // real, underscore at position 3
		"gi9bG__TnJA", // real, double underscore
		"ZIP_AAAAAAA", // shaped like a fabricated prefix, but a legal 11-char ID
		"city_XYZabc", // same, lowercase
	} {
		if err := validateMetricsGeoID(cfg, geoID); err != nil {
			t.Errorf("expected opaque geo_id %q to pass through to the API, got %v", geoID, err)
		}
	}
}

// geoMetricsCommands returns the geo metrics commands as they are wired into
// the real command tree: every current or monthly command under a metrics
// group. Reading the tree, rather than a list kept by hand, means a ninth geo
// metrics command is covered the moment it is registered.
func geoMetricsCommands(t *testing.T) []*cobra.Command {
	t.Helper()
	var cmds []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if (sub.Name() == "current" || sub.Name() == "monthly") && sub.Parent().Name() == "metrics" {
				cmds = append(cmds, sub)
			}
			walk(sub)
		}
	}
	walk(rootCmd)
	if len(cmds) != 8 {
		t.Fatalf("expected the 8 geo metrics commands in the tree, found %d", len(cmds))
	}
	return cmds
}

// A caller who still passes --include-count gets the parser's unknown-flag
// error, the same answer any misspelled flag gets. The real commands are
// parsed, so a flag re-added anywhere, in the shared registrar or in a geo
// file's init, is caught. Only the one flag is parsed, so nothing is set on
// the shared command tree.
func TestGeoMetricsCommandsRejectIncludeCount(t *testing.T) {
	for _, cmd := range geoMetricsCommands(t) {
		t.Run(commandPathFromCobra(cmd), func(t *testing.T) {
			err := cmd.ParseFlags([]string{"--include-count"})

			if err == nil {
				t.Fatal("accepted --include-count, which no metrics endpoint supports")
			}
			if !strings.Contains(err.Error(), "unknown flag: --include-count") {
				t.Errorf("expected an unknown-flag error naming --include-count, got %q", err)
			}
		})
	}
}

// geoMetricsCase names one of the eight geo metrics commands by the
// configuration it is built from, so the request it builds can be run
// through --dry-run in isolation.
type geoMetricsCase struct {
	name    string
	cfg     metricsConfig
	monthly bool
}

func geoMetricsCases() []geoMetricsCase {
	var cases []geoMetricsCase
	for _, g := range []struct {
		resource string
		cfg      metricsConfig
	}{
		{"addresses", addressesMetricsCfg},
		{"cities", citiesMetricsCfg},
		{"counties", countiesMetricsCfg},
		{"jurisdictions", jurisdictionsMetricsCfg},
	} {
		cases = append(cases,
			geoMetricsCase{name: g.resource + " metrics current", cfg: g.cfg},
			geoMetricsCase{name: g.resource + " metrics monthly", cfg: g.cfg, monthly: true},
		)
	}
	return cases
}

// newTestGeoMetricsCmd builds one geo metrics command the way the geo files
// do, the shared registrar plus the global flags the run function reads, with
// stdout captured, so the request it would send can be read back through
// --dry-run without a server and without the root command's pre-run.
func newTestGeoMetricsCmd(c geoMetricsCase) (*cobra.Command, *bytes.Buffer) {
	cmd := &cobra.Command{
		Use:           "metrics GEO_ID",
		Args:          exactArgsUnlessSchema(1),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	if c.monthly {
		cmd.RunE = runMetricsMonthly(c.cfg)
		registerMetricsMonthlyFlags(cmd, c.cfg.requirePropertyType)
	} else {
		cmd.RunE = runMetricsCurrent(c.cfg)
		registerMetricsCurrentFlags(cmd, c.cfg.requirePropertyType)
	}
	standInGlobalFlags(cmd)

	var out bytes.Buffer
	cmd.SetOut(&out)
	return cmd, &out
}

// requiredGeoMetricsArgs returns a geo_id and every flag the command requires.
func requiredGeoMetricsArgs(c geoMetricsCase) []string {
	args := []string{"aGeo1OpaqueId", "--tag", "solar"}
	if c.cfg.requirePropertyType {
		args = append(args, "--property-type", "residential")
	}
	if c.monthly {
		args = append(args, "--metric-from", "2024-01-01", "--metric-to", "2024-12-31")
	}
	return args
}

// The request a metrics command builds never carries include_count: the
// parameter would be rejected once the API refuses unknown query parameters.
func TestGeoMetricsCommandsNeverSendIncludeCount(t *testing.T) {
	for _, c := range geoMetricsCases() {
		t.Run(c.name, func(t *testing.T) {
			cmd, out := newTestGeoMetricsCmd(c)
			cmd.SetArgs(append(requiredGeoMetricsArgs(c), "--dry-run"))

			err := cmd.Execute()

			if err != nil {
				t.Fatalf("%s --dry-run failed: %v", c.name, err)
			}
			var req dryRunRequest
			if err := json.Unmarshal(out.Bytes(), &req); err != nil {
				t.Fatalf("dry-run output is not JSON: %v\n%s", err, out.String())
			}
			if v, ok := req.Params["include_count"]; ok {
				t.Errorf("%s sends include_count=%v, which no metrics endpoint accepts", c.name, v)
			}
		})
	}
}
