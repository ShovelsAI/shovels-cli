package output

import (
	"encoding/json"
	"io"

	"github.com/shovels-ai/shovels-cli/internal/client"
)

// PrintPaginated writes a JSON envelope for a paginated result. The data
// field contains the items array, and meta includes count, has_more, and
// credit information aggregated across all pages in the pagination sequence.
// meta.trust_summaries carries the per-page trust summaries unread and
// unaggregated: each is emitted as the same JSON value the API returned --
// keys, values, numbers, strings, arrays and nulls all survive -- though the
// encoder may change its byte-level formatting. The key is omitted entirely
// when no fetched page carried a summary.
//
// meta.server_capped is the endpoint's fixed record ceiling, present only when
// the endpoint caps its result set. The disclosure describes the endpoint
// rather than the result, so a query matching fewer records than the ceiling
// carries it too. A reader never sees the key beside a live continuation: a
// capped endpoint returns no cursor, so has_more is false wherever the key
// appears.
func PrintPaginated(w io.Writer, result *client.PaginatedResult) {
	meta := map[string]any{
		"count":    len(result.Items),
		"has_more": result.HasMore,
	}
	if result.CreditsUsed != nil {
		meta["credits_used"] = *result.CreditsUsed
	}
	if result.CreditsRemaining != nil {
		meta["credits_remaining"] = *result.CreditsRemaining
	}
	if result.TotalCount != nil {
		meta["total_count"] = result.TotalCount
	}
	if len(result.TrustSummaries) > 0 {
		meta["trust_summaries"] = result.TrustSummaries
	}
	if result.ServerCap > 0 {
		meta["server_capped"] = result.ServerCap
	}

	env := Envelope{
		Data: result.Items,
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
