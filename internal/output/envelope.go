package output

import (
	"encoding/json"
	"io"

	"github.com/shovels-ai/shovels-cli/internal/client"
)

// PrintPaginated writes a JSON envelope for paginated responses. The data
// field contains the items array, and meta includes count, has_more, and
// credit information from the last API response in the pagination sequence.
func PrintPaginated(w io.Writer, items []json.RawMessage, hasMore bool, credits client.CreditMeta) {
	meta := map[string]any{
		"count":    len(items),
		"has_more": hasMore,
	}
	if credits.CreditsUsed != nil {
		meta["credits_used"] = *credits.CreditsUsed
	}
	if credits.CreditsRemaining != nil {
		meta["credits_remaining"] = *credits.CreditsRemaining
	}

	env := Envelope{
		Data: items,
		Meta: meta,
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(env)
}

// PrintBatch writes a JSON envelope for non-paginated batch responses. The
// data field contains the items array, meta includes count and credit
// information, and meta.missing lists any requested IDs not found in the
// response. The missing field is omitted entirely when all IDs are found.
func PrintBatch(w io.Writer, items []json.RawMessage, missing []string, credits client.CreditMeta) {
	meta := map[string]any{
		"count": len(items),
	}
	if len(missing) > 0 {
		meta["missing"] = missing
	}
	if credits.CreditsUsed != nil {
		meta["credits_used"] = *credits.CreditsUsed
	}
	if credits.CreditsRemaining != nil {
		meta["credits_remaining"] = *credits.CreditsRemaining
	}

	env := Envelope{
		Data: items,
		Meta: meta,
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(env)
}

// PrintSingle writes a JSON envelope for non-paginated (single object) API
// responses. The data field contains the object, and meta includes credit
// information. No count or has_more fields are included.
func PrintSingle(w io.Writer, data any, credits client.CreditMeta) {
	meta := map[string]any{}
	if credits.CreditsUsed != nil {
		meta["credits_used"] = *credits.CreditsUsed
	}
	if credits.CreditsRemaining != nil {
		meta["credits_remaining"] = *credits.CreditsRemaining
	}

	env := Envelope{
		Data: data,
		Meta: meta,
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(env)
}
