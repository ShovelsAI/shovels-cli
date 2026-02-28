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

func TestPrintErrorProducesValidJSON(t *testing.T) {
	var buf bytes.Buffer
	PrintError(&buf, "unknown command", 1)

	var payload ErrorPayload
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw: %s", err, buf.String())
	}

	if payload.Error != "unknown command" {
		t.Errorf("expected error %q, got %q", "unknown command", payload.Error)
	}
	if payload.Code != 1 {
		t.Errorf("expected code 1, got %d", payload.Code)
	}
}

func TestPrintErrorContainsOnlyErrorAndCode(t *testing.T) {
	var buf bytes.Buffer
	PrintError(&buf, "test error", 2)

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(raw) != 2 {
		t.Errorf("expected exactly 2 keys, got %d: %v", len(raw), raw)
	}
	if _, ok := raw["error"]; !ok {
		t.Error("missing 'error' key")
	}
	if _, ok := raw["code"]; !ok {
		t.Error("missing 'code' key")
	}
}

func TestPrintErrorTypedIncludesErrorType(t *testing.T) {
	var buf bytes.Buffer
	PrintErrorTyped(&buf, "Unauthorized", 2, "auth_error")

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	if len(raw) != 3 {
		t.Errorf("expected exactly 3 keys, got %d: %v", len(raw), raw)
	}

	var payload ErrorPayload
	if err := json.Unmarshal(buf.Bytes(), &payload); err != nil {
		t.Fatalf("cannot unmarshal: %v", err)
	}
	if payload.ErrorType != "auth_error" {
		t.Errorf("expected error_type %q, got %q", "auth_error", payload.ErrorType)
	}
	if payload.Error != "Unauthorized" {
		t.Errorf("expected error %q, got %q", "Unauthorized", payload.Error)
	}
	if payload.Code != 2 {
		t.Errorf("expected code 2, got %d", payload.Code)
	}
}
