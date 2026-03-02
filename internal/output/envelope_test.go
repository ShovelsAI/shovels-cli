package output

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/shovels-ai/shovels-cli/internal/client"
)

func intPtr(n int) *int { return &n }

func TestPrintPaginatedEnvelopeShape(t *testing.T) {
	var buf bytes.Buffer
	items := []json.RawMessage{
		json.RawMessage(`{"id":1}`),
		json.RawMessage(`{"id":2}`),
	}
	credits := client.CreditMeta{
		CreditsUsed:      intPtr(50),
		CreditsRemaining: intPtr(9950),
	}

	PrintPaginated(&buf, items, true, credits)

	var env struct {
		Data []json.RawMessage `json:"data"`
		Meta map[string]any    `json:"meta"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}

	if len(env.Data) != 2 {
		t.Errorf("expected 2 items in data, got %d", len(env.Data))
	}

	count, ok := env.Meta["count"]
	if !ok {
		t.Fatal("expected count in meta")
	}
	if int(count.(float64)) != 2 {
		t.Errorf("expected count=2, got %v", count)
	}

	hasMore, ok := env.Meta["has_more"]
	if !ok {
		t.Fatal("expected has_more in meta")
	}
	if hasMore != true {
		t.Errorf("expected has_more=true, got %v", hasMore)
	}

	cu, ok := env.Meta["credits_used"]
	if !ok {
		t.Fatal("expected credits_used in meta")
	}
	if int(cu.(float64)) != 50 {
		t.Errorf("expected credits_used=50, got %v", cu)
	}

	cr, ok := env.Meta["credits_remaining"]
	if !ok {
		t.Fatal("expected credits_remaining in meta")
	}
	if int(cr.(float64)) != 9950 {
		t.Errorf("expected credits_remaining=9950, got %v", cr)
	}
}

func TestPrintPaginatedNoCredits(t *testing.T) {
	var buf bytes.Buffer
	items := []json.RawMessage{json.RawMessage(`{"id":1}`)}
	credits := client.CreditMeta{}

	PrintPaginated(&buf, items, false, credits)

	var env struct {
		Data []json.RawMessage `json:"data"`
		Meta map[string]any    `json:"meta"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if _, ok := env.Meta["credits_used"]; ok {
		t.Error("expected credits_used to be absent when nil")
	}
	if _, ok := env.Meta["credits_remaining"]; ok {
		t.Error("expected credits_remaining to be absent when nil")
	}
}

func TestPrintPaginatedCountEqualsActualItems(t *testing.T) {
	var buf bytes.Buffer
	items := []json.RawMessage{
		json.RawMessage(`{"id":1}`),
		json.RawMessage(`{"id":2}`),
		json.RawMessage(`{"id":3}`),
	}

	PrintPaginated(&buf, items, false, client.CreditMeta{})

	var env struct {
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	count := int(env.Meta["count"].(float64))
	if count != 3 {
		t.Errorf("expected count=3 (actual items), got %d", count)
	}
}

func TestPrintBatchEnvelopeShape(t *testing.T) {
	var buf bytes.Buffer
	items := []json.RawMessage{
		json.RawMessage(`{"id":"P1"}`),
		json.RawMessage(`{"id":"P2"}`),
	}
	credits := client.CreditMeta{
		CreditsUsed:      intPtr(2),
		CreditsRemaining: intPtr(9998),
	}

	PrintBatch(&buf, items, nil, credits)

	var env struct {
		Data []json.RawMessage `json:"data"`
		Meta map[string]any    `json:"meta"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}

	if len(env.Data) != 2 {
		t.Errorf("expected 2 items in data, got %d", len(env.Data))
	}
	if int(env.Meta["count"].(float64)) != 2 {
		t.Errorf("expected count=2, got %v", env.Meta["count"])
	}
	if _, ok := env.Meta["has_more"]; ok {
		t.Error("batch response should not have has_more in meta")
	}
	if _, ok := env.Meta["missing"]; ok {
		t.Error("missing should be absent when all IDs found")
	}
	if int(env.Meta["credits_used"].(float64)) != 2 {
		t.Errorf("expected credits_used=2, got %v", env.Meta["credits_used"])
	}
	if int(env.Meta["credits_remaining"].(float64)) != 9998 {
		t.Errorf("expected credits_remaining=9998, got %v", env.Meta["credits_remaining"])
	}
}

func TestPrintBatchWithMissing(t *testing.T) {
	var buf bytes.Buffer
	items := []json.RawMessage{
		json.RawMessage(`{"id":"P1"}`),
	}
	missing := []string{"P999", "P888"}
	credits := client.CreditMeta{
		CreditsUsed:      intPtr(1),
		CreditsRemaining: intPtr(9999),
	}

	PrintBatch(&buf, items, missing, credits)

	var env struct {
		Data []json.RawMessage `json:"data"`
		Meta map[string]any    `json:"meta"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}

	if len(env.Data) != 1 {
		t.Errorf("expected 1 item in data, got %d", len(env.Data))
	}
	if int(env.Meta["count"].(float64)) != 1 {
		t.Errorf("expected count=1, got %v", env.Meta["count"])
	}

	missingVal, ok := env.Meta["missing"]
	if !ok {
		t.Fatal("expected missing in meta")
	}
	missingArr, ok := missingVal.([]any)
	if !ok {
		t.Fatalf("expected missing to be array, got %T", missingVal)
	}
	if len(missingArr) != 2 {
		t.Errorf("expected 2 missing IDs, got %d", len(missingArr))
	}
	if missingArr[0].(string) != "P999" || missingArr[1].(string) != "P888" {
		t.Errorf("expected missing [P999, P888], got %v", missingArr)
	}
}

func TestPrintBatchEmptyMissingSliceOmitted(t *testing.T) {
	var buf bytes.Buffer
	PrintBatch(&buf, []json.RawMessage{json.RawMessage(`{"id":"P1"}`)}, []string{}, client.CreditMeta{})

	var env struct {
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if _, ok := env.Meta["missing"]; ok {
		t.Error("missing should be absent when empty slice provided")
	}
}

func TestPrintSingleEnvelopeShape(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]any{
		"credits_used":  5432,
		"credit_limit":  10000,
		"is_over_limit": false,
	}
	credits := client.CreditMeta{
		CreditsUsed:      intPtr(0),
		CreditsRemaining: intPtr(10000),
	}

	PrintSingle(&buf, data, credits)

	var env struct {
		Data map[string]any `json:"data"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, buf.String())
	}

	// Data should be the original object.
	if env.Data["credits_used"] == nil {
		t.Error("expected credits_used in data")
	}

	// Meta should have credits but NO count or has_more.
	if _, ok := env.Meta["count"]; ok {
		t.Error("non-paginated response should not have count in meta")
	}
	if _, ok := env.Meta["has_more"]; ok {
		t.Error("non-paginated response should not have has_more in meta")
	}
	if _, ok := env.Meta["credits_used"]; !ok {
		t.Error("expected credits_used in meta")
	}
}

func TestPrintSingleNoCredits(t *testing.T) {
	var buf bytes.Buffer
	PrintSingle(&buf, map[string]string{"key": "value"}, client.CreditMeta{})

	var env struct {
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	// Meta should be empty but not nil.
	if len(env.Meta) != 0 {
		t.Errorf("expected empty meta for no-credit single response, got %v", env.Meta)
	}
}
