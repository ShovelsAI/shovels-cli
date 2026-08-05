package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pinnedSpecPath is the committed copy of the OpenAPI spec, the same fixture
// the generator can be pointed at via SCHEMA_GEN_SPEC_FILE. The guard test
// reads it rather than fetching the live spec: this is the unit suite, which
// CLAUDE.md documents as making no network calls, and a test that skips itself
// when the network is unreachable reports success for zero coverage.
const pinnedSpecPath = "testdata/openapi.json"

// TestSchemaGenFailsOnMissingResponseSchema proves the generator's loud-fail
// guard fires: when a coverage command names a response schema that is absent
// from the OpenAPI spec, `go generate` (here, running schema_gen.go directly)
// must exit non-zero and the error must name both the offending command and
// the unresolved schema. This is the regression for the "Error conditions"
// behavior in Step 2 — without the guard, a bad schema name would silently
// produce an empty schema.
//
// The test takes the pinned spec, strips the CoverageItem schema, and feeds the
// modified spec back to the generator via SCHEMA_GEN_SPEC_FILE so the failure
// is forced onto a coverage command deterministically and offline.
func TestSchemaGenFailsOnMissingResponseSchema(t *testing.T) {
	spec := readPinnedSpec(t)

	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("spec has no components object")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("spec has no components.schemas object")
	}
	if _, ok := schemas["CoverageItem"]; !ok {
		t.Fatal("pinned spec unexpectedly missing CoverageItem; cannot exercise guard")
	}
	delete(schemas, "CoverageItem")

	specPath := filepath.Join(t.TempDir(), "openapi_no_coverage.json")
	modified, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal modified spec: %v", err)
	}
	if err := os.WriteFile(specPath, modified, 0o644); err != nil {
		t.Fatalf("write fixture spec: %v", err)
	}

	cmd := exec.Command("go", "run", "schema_gen.go")
	cmd.Env = append(os.Environ(), "SCHEMA_GEN_SPEC_FILE="+specPath)
	out, err := cmd.CombinedOutput()

	if err == nil {
		t.Fatalf("expected generator to exit non-zero when CoverageItem is absent; output:\n%s", out)
	}

	combined := string(out)
	if !strings.Contains(combined, "coverage") {
		t.Errorf("error should name a coverage command, got:\n%s", combined)
	}
	if !strings.Contains(combined, "CoverageItem") {
		t.Errorf("error should name the unresolved schema CoverageItem, got:\n%s", combined)
	}
	if !strings.Contains(combined, "resolved to zero fields") {
		t.Errorf("error should explain the schema resolved to zero fields, got:\n%s", combined)
	}
}

// readPinnedSpec loads the committed OpenAPI fixture. Failing rather than
// skipping is deliberate: the fixture is in the repo, so an unreadable one is a
// real problem, not an environmental one.
//
// Whether the pinned spec still matches production is a live question and so
// belongs in the integration suite, which asserts the declared shapes of the
// parameters the CLI encodes as repeated keys.
func readPinnedSpec(t *testing.T) map[string]any {
	t.Helper()

	body, err := os.ReadFile(pinnedSpecPath)
	if err != nil {
		t.Fatalf("read pinned spec %s: %v", pinnedSpecPath, err)
	}

	var spec map[string]any
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatalf("parse pinned spec JSON: %v", err)
	}
	return spec
}
