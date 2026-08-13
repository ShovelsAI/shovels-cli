//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// A transport-only command bypasses the paginator, so its result set is
// whatever the endpoint returned, in the order it returned it. The case below
// compares the whole set element by element against what the stub served: a
// bound or a reordering between the response and stdout reads as a different
// set rather than as an error, and only the full comparison sees it.

// --- Happy paths ---

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
