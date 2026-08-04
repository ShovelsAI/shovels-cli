//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pinnedSpecRelPath is the OpenAPI spec that cmd/schema_data.go is generated
// from, stored as the API serves it so it can be checked against production
// with curl and diff. The header of cmd/schema_gen.go carries the sequence
// that refreshes it and everything pinned against it.
const pinnedSpecRelPath = "cmd/testdata/openapi.json"

// generatedSchemaRelPath is the generator's output artifact.
const generatedSchemaRelPath = "cmd/schema_data.go"

// TestSchemaRegenerationReproducesCommittedData runs the generator over the
// pinned spec in a scratch copy of the module and requires the result to equal
// the committed cmd/schema_data.go byte for byte. A mismatch means the
// generator is not deterministic, or the committed artifact was hand-edited
// rather than generated, or the spec and the data were refreshed apart.
//
// The spec comes from the checked-in copy rather than the network: a live
// fetch would turn unrelated API releases into failures of this repo.
func TestSchemaRegenerationReproducesCommittedData(t *testing.T) {
	root := moduleRoot()

	spec := filepath.Join(root, filepath.FromSlash(pinnedSpecRelPath))
	if _, err := os.Stat(spec); err != nil {
		t.Fatalf("pinned spec fixture unreadable: %v", err)
	}

	work := t.TempDir()
	copyModuleTree(t, root, work)

	gen := exec.Command("go", "generate", "./cmd/...")
	gen.Dir = work
	gen.Env = append(os.Environ(), "SCHEMA_GEN_SPEC_FILE="+spec)
	out, err := gen.CombinedOutput()
	if err != nil {
		t.Fatalf("go generate ./cmd/... failed: %v\n%s", err, out)
	}

	committed := readFile(t, filepath.Join(root, filepath.FromSlash(generatedSchemaRelPath)))
	regenerated := readFile(t, filepath.Join(work, filepath.FromSlash(generatedSchemaRelPath)))

	// The recovery is a sequence, not one command, so the failure points at
	// the one place that sequence is written down.
	if !bytes.Equal(committed, regenerated) {
		t.Errorf("regenerating from %s does not reproduce the committed %s\n%s\nrefresh sequence: see the header of cmd/schema_gen.go",
			pinnedSpecRelPath, generatedSchemaRelPath, firstLineDifference(committed, regenerated))
	}
}

// readFile reads a whole file, failing the test when it cannot.
func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// firstLineDifference describes where two files start to disagree, so a
// failure names the drifting entry instead of two byte counts.
func firstLineDifference(committed, regenerated []byte) string {
	want := strings.Split(string(committed), "\n")
	got := strings.Split(string(regenerated), "\n")

	for i := 0; i < len(want) && i < len(got); i++ {
		if want[i] != got[i] {
			return fmt.Sprintf("line %d:\n  committed:   %s\n  regenerated: %s", i+1, want[i], got[i])
		}
	}
	return fmt.Sprintf("committed has %d lines, regenerated has %d", len(want), len(got))
}

// copyModuleTree copies the module source tree into dst so the generator can
// write its output without touching the working tree. Dot-prefixed entries
// hold version-control and tooling state that the go command does not read.
func copyModuleTree(t *testing.T, src, dst string) {
	t.Helper()

	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatalf("copy module tree: %v", err)
	}
}
