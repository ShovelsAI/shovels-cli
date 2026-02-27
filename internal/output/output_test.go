package output

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestPrintDataProducesValidJSONEnvelope(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"version": "1.0.0"}

	PrintData(&buf, data)

	var envelope Envelope
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, buf.String())
	}

	if envelope.Meta == nil {
		t.Error("meta should not be nil")
	}
}

func TestPrintDataIncludesDataField(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]string{"key": "value"}

	PrintData(&buf, data)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if _, ok := raw["data"]; !ok {
		t.Error("missing 'data' key in envelope")
	}
	if _, ok := raw["meta"]; !ok {
		t.Error("missing 'meta' key in envelope")
	}
}

func TestPrintDataMetaIsEmptyObject(t *testing.T) {
	var buf bytes.Buffer
	PrintData(&buf, "hello")

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	// meta should be {} not null
	if string(raw["meta"]) != "{}" {
		t.Errorf("expected meta to be {}, got %s", string(raw["meta"]))
	}
}
