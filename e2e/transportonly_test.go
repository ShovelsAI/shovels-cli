//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The transport-only commands bypass the paginator, so their result set is
// whatever the endpoint returned, in the order it returned it. Each case
// compares the whole set element by element against what the stub served: a
// bound or a reordering between the response and stdout reads as a different
// set rather than as an error, and only the full comparison sees it.

// monthlyMetricRow is one month of a contractor's metrics.
type monthlyMetricRow struct {
	Month       string `json:"month"`
	PermitCount int    `json:"permit_count"`
}

// monthlyRows returns one row per month from January of firstYear through
// December of lastYear. Every row carries a distinct permit count, so a
// comparison against them catches a reordering as well as a missing row.
func monthlyRows(firstYear, lastYear int) []monthlyMetricRow {
	var rows []monthlyMetricRow
	for year := firstYear; year <= lastYear; year++ {
		for month := 1; month <= 12; month++ {
			rows = append(rows, monthlyMetricRow{
				Month:       fmt.Sprintf("%d-%02d", year, month),
				PermitCount: len(rows) + 1,
			})
		}
	}
	return rows
}

// makeMonthlyMetricsHandler serves rows as the metrics endpoint's item array.
func makeMonthlyMetricsHandler(rows []monthlyMetricRow) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := struct {
			Items []monthlyMetricRow `json:"items"`
		}{Items: rows}
		json.NewEncoder(w).Encode(resp)
	})
}

// --- Happy paths ---

// The window spans more months than root's default --limit, which is the bound a
// rewiring through the paginator would impose with no flag asking for one. A
// shorter window returns whole under either wiring and proves nothing.
func TestContractorsMetricsReturnsEveryMonthlyRow(t *testing.T) {
	served := monthlyRows(2020, 2024)
	srv := httptest.NewServer(makeMonthlyMetricsHandler(served))
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"contractors", "metrics", "ABC123",
		"--metric-from", "2020-01-01",
		"--metric-to", "2024-12-31",
		"--tag", "solar",
		"--property-type", "residential",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	parsed := parseEnvelope(t, result.Stdout)
	var returned []monthlyMetricRow
	if err := json.Unmarshal(parsed.Data, &returned); err != nil {
		t.Fatalf("expected an array of monthly rows: %v\ndata: %s", err, parsed.Data)
	}

	if len(returned) != len(served) {
		t.Fatalf("expected all %d monthly rows, got %d", len(served), len(returned))
	}
	for i, row := range served {
		if returned[i] != row {
			t.Errorf("row %d: expected %+v, got %+v", i, row, returned[i])
		}
	}
}

// --- Edge cases ---

func TestStatesCoverageReturnsEveryRow(t *testing.T) {
	served := []coverageItem{
		{Field: "fees", Tier: "missing", FillPct: 0.02, PermitsTotal: 12034},
		{Field: "job_value", Tier: "partial", FillPct: 0.41, PermitsTotal: 12034},
		{Field: "contractor_name", Tier: "partial", FillPct: 0.63, PermitsTotal: 12034},
	}
	handler, _, _ := makeCoverageHandler(served)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"states", "coverage", "CA",
		"--coverage-from", "2024-01-01",
		"--coverage-to", "2024-12-31",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}
	parsed := parseEnvelope(t, result.Stdout)
	var returned []coverageItem
	if err := json.Unmarshal(parsed.Data, &returned); err != nil {
		t.Fatalf("expected an array of coverage rows: %v\ndata: %s", err, parsed.Data)
	}

	if len(returned) != len(served) {
		t.Fatalf("expected all %d coverage rows, got %d", len(served), len(returned))
	}
	for i, row := range served {
		if returned[i] != row {
			t.Errorf("row %d: expected %+v, got %+v", i, row, returned[i])
		}
	}
}
