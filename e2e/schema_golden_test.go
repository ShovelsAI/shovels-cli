//go:build e2e

package e2e

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"testing"
)

// updateSchemaGolden rewrites the pinned schema output instead of comparing
// against it. Use it when a schema change is intended:
//
//	go test -tags=e2e ./e2e/... -run TestSchemaOutputMatchesGolden -update-schema-golden
//
// A change that came from the API rather than from this repo is the last step
// of a longer sequence, written down in the header of cmd/schema_gen.go.
var updateSchemaGolden = flag.Bool("update-schema-golden", false,
	"rewrite the pinned schema output from the current binary")

// schemaGoldenPath holds one command's --schema document per line, sorted by
// command, so a diff of the file names the commands whose output moved.
const schemaGoldenPath = "testdata/schema_golden.jsonl"

// goldenUpdateHint tells a reader of a failure how to accept the new output,
// which is the only way past this test and should therefore be a deliberate
// act rather than a search.
const goldenUpdateHint = "rerun with -update-schema-golden once the output is intended"

// TestSchemaOutputMatchesGolden compares the full --schema document of every
// registered command against its pinned copy. The comparison is structural
// and complete: any change to a command's fields, types, descriptions, units,
// filters, or field index fails here, including changes reaching a command
// through shared generator code rather than through its own entry.
func TestSchemaOutputMatchesGolden(t *testing.T) {
	current := currentSchemaOutputs(t)

	if *updateSchemaGolden {
		writeSchemaGolden(t, current)
	}

	pinned := readSchemaGolden(t)

	for _, command := range sortedKeys(current) {
		if _, ok := pinned[command]; !ok {
			t.Errorf("command %q has no pinned schema; %s", command, goldenUpdateHint)
		}
	}
	for _, command := range sortedKeys(pinned) {
		if _, ok := current[command]; !ok {
			t.Errorf("pinned schema %q matches no registered command", command)
		}
	}

	for _, command := range sortedKeys(current) {
		want, ok := pinned[command]
		if !ok {
			continue
		}
		if got := current[command]; got != want {
			t.Errorf("--schema output for %q changed; %s\n%s",
				command, firstJSONDifference(want, got), goldenUpdateHint)
		}
	}
}

// currentSchemaOutputs returns every registered command's --schema document,
// canonicalized for comparison.
func currentSchemaOutputs(t *testing.T) map[string]string {
	t.Helper()

	outputs := make(map[string]string)
	for _, command := range registeredSchemaCommands(t) {
		stdout := mustSchema(t, nil, strings.Split(command, " ")...)
		outputs[command] = canonicalJSON(t, stdout)
	}
	return outputs
}

// registeredSchemaCommands returns the command paths the schema listing
// advertises.
func registeredSchemaCommands(t *testing.T) []string {
	t.Helper()

	result := runCLI(t, "schema")
	if result.ExitCode != 0 {
		t.Fatalf("schema listing failed: exit %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	var commands []string
	if err := json.Unmarshal([]byte(result.Stdout), &commands); err != nil {
		t.Fatalf("schema listing is not a JSON array: %v\nstdout: %s", err, result.Stdout)
	}
	if len(commands) == 0 {
		t.Fatal("schema listing is empty")
	}
	return commands
}

// canonicalJSON re-encodes a JSON document so two encodings of the same data
// compare equal. Numbers keep their source spelling, so a type change from
// integer to float is not hidden by a round trip through float64.
func canonicalJSON(t *testing.T, document string) string {
	t.Helper()

	decoder := json.NewDecoder(strings.NewReader(document))
	decoder.UseNumber()

	var parsed any
	if err := decoder.Decode(&parsed); err != nil {
		t.Fatalf("schema output is not valid JSON: %v\noutput: %s", err, document)
	}

	encoded, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("re-encode schema output: %v", err)
	}
	return string(encoded)
}

// firstJSONDifference reports where two canonical documents diverge, with
// enough surrounding text to name the entry. Keys are sorted, so the window
// opens on the field that moved rather than on the whole document.
func firstJSONDifference(pinned, current string) string {
	shared := 0
	for shared < len(pinned) && shared < len(current) && pinned[shared] == current[shared] {
		shared++
	}

	const lead = 60
	start := shared - lead
	if start < 0 {
		start = 0
	}
	return fmt.Sprintf("first difference at byte %d\n  pinned:  %s\n  current: %s",
		shared, jsonWindow(pinned, start), jsonWindow(current, start))
}

// jsonWindow returns a bounded slice of a document starting at start.
func jsonWindow(document string, start int) string {
	const width = 200
	if start > len(document) {
		return ""
	}
	end := start + width
	if end > len(document) {
		end = len(document)
	}
	return document[start:end]
}

// readSchemaGolden loads the pinned schema documents, canonicalized so
// reformatting the file does not change what it asserts.
func readSchemaGolden(t *testing.T) map[string]string {
	t.Helper()

	file, err := os.Open(schemaGoldenPath)
	if err != nil {
		t.Fatalf("open %s: %v", schemaGoldenPath, err)
	}
	defer file.Close()

	pinned := make(map[string]string)
	decoder := json.NewDecoder(file)
	for {
		var document json.RawMessage
		if err := decoder.Decode(&document); err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("parse %s: %v", schemaGoldenPath, err)
		}

		var header struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(document, &header); err != nil {
			t.Fatalf("parse %s: %v", schemaGoldenPath, err)
		}
		if header.Command == "" {
			t.Fatalf("%s has an entry with no command", schemaGoldenPath)
		}
		if _, duplicate := pinned[header.Command]; duplicate {
			t.Fatalf("%s pins %q twice", schemaGoldenPath, header.Command)
		}
		pinned[header.Command] = canonicalJSON(t, string(document))
	}
	if len(pinned) == 0 {
		t.Fatalf("%s pins no commands", schemaGoldenPath)
	}
	return pinned
}

// writeSchemaGolden rewrites the pinned schema documents from the given
// outputs.
func writeSchemaGolden(t *testing.T, outputs map[string]string) {
	t.Helper()

	var buf strings.Builder
	for _, command := range sortedKeys(outputs) {
		fmt.Fprintf(&buf, "%s\n", outputs[command])
	}

	if err := os.WriteFile(schemaGoldenPath, []byte(buf.String()), 0o644); err != nil {
		t.Fatalf("write %s: %v", schemaGoldenPath, err)
	}
}

// sortedKeys returns a map's keys in a stable order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
