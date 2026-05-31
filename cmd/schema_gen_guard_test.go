package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openAPISpecURL mirrors the generator's spec source so the guard test can
// fetch the same live spec and modify it before feeding it back in.
const openAPISpecURL = "https://api.shovels.ai/v2/openapi.json"

// TestSchemaGenFailsOnMissingResponseSchema proves the generator's loud-fail
// guard fires: when a coverage command names a response schema that is absent
// from the OpenAPI spec, `go generate` (here, running schema_gen.go directly)
// must exit non-zero and the error must name both the offending command and
// the unresolved schema. This is the regression for the "Error conditions"
// behavior in Step 2 — without the guard, a bad schema name would silently
// produce an empty schema.
//
// The test takes the live spec, strips the CoverageItem schema, and feeds the
// modified spec back to the generator via SCHEMA_GEN_SPEC_FILE so the failure
// is forced onto a coverage command deterministically and offline thereafter.
func TestSchemaGenFailsOnMissingResponseSchema(t *testing.T) {
	spec := fetchLiveSpec(t)

	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("spec has no components object")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("spec has no components.schemas object")
	}
	if _, ok := schemas["CoverageItem"]; !ok {
		t.Fatal("live spec unexpectedly missing CoverageItem; cannot exercise guard")
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

// fetchLiveSpec downloads the live OpenAPI spec the generator uses. The test
// skips (rather than fails) when the network is unreachable, matching the
// generator's own dependency on a reachable spec at generate time.
func fetchLiveSpec(t *testing.T) map[string]any {
	t.Helper()

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(openAPISpecURL)
	if err != nil {
		t.Skipf("OpenAPI spec unreachable, skipping guard test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Skipf("OpenAPI spec returned status %d, skipping guard test", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read spec body: %v", err)
	}

	var spec map[string]any
	if err := json.Unmarshal(body, &spec); err != nil {
		t.Fatalf("parse spec JSON: %v", err)
	}
	return spec
}
