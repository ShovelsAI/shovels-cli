package client

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// MaxCeiling is the absolute upper bound for both --limit and --max-records.
// All code paths enforce this ceiling to prevent unbounded memory growth.
const MaxCeiling = 100_000

// DefaultMaxRecords is the default cap when --limit=all is used.
const DefaultMaxRecords = 10_000

// apiPageSizeMax is the maximum page size the API accepts per request.
const apiPageSizeMax = 50

// LimitConfig holds the parsed --limit and --max-records values.
type LimitConfig struct {
	// Limit is the requested record count, or 0 for "all".
	Limit int
	// All is true when --limit=all was specified.
	All bool
	// MaxRecords is the cap for --limit=all mode.
	MaxRecords int
}

// ParseLimit validates and parses the --limit flag value. Accepts a positive
// integer (1 to MaxCeiling) or the string "all". Returns an error for zero,
// negative, non-numeric, or values exceeding the ceiling.
func ParseLimit(raw string) (LimitConfig, error) {
	raw = strings.TrimSpace(raw)
	if strings.EqualFold(raw, "all") {
		return LimitConfig{All: true, MaxRecords: DefaultMaxRecords}, nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil {
		return LimitConfig{}, fmt.Errorf("invalid limit %q: must be a positive integer or \"all\"", raw)
	}
	if n <= 0 {
		return LimitConfig{}, fmt.Errorf("invalid limit %d: must be a positive integer or \"all\"", n)
	}
	if n > MaxCeiling {
		return LimitConfig{}, fmt.Errorf("limit cannot exceed %d", MaxCeiling)
	}

	return LimitConfig{Limit: n}, nil
}

// ValidateMaxRecords checks --max-records against the ceiling.
func ValidateMaxRecords(maxRecords int) error {
	if maxRecords <= 0 {
		return fmt.Errorf("max-records must be a positive integer")
	}
	if maxRecords > MaxCeiling {
		return fmt.Errorf("max-records cannot exceed %d", MaxCeiling)
	}
	return nil
}

// WithMaxRecords sets the MaxRecords cap on the config. Only meaningful when
// All is true. Values exceeding MaxCeiling are clamped silently to prevent
// unbounded memory growth regardless of how the caller obtained the value.
func (lc LimitConfig) WithMaxRecords(maxRecords int) LimitConfig {
	if maxRecords > MaxCeiling {
		maxRecords = MaxCeiling
	}
	lc.MaxRecords = maxRecords
	return lc
}

// EffectiveLimit returns the total number of records to fetch.
func (lc LimitConfig) EffectiveLimit() int {
	if lc.All {
		return lc.MaxRecords
	}
	return lc.Limit
}

// pageResponse is the expected shape of a paginated API response.
type pageResponse struct {
	Items      []json.RawMessage `json:"items"`
	NextCursor *string           `json:"next_cursor"`
}

// PaginatedResult holds the assembled output from paginating through an API
// endpoint. All items are collected in memory before returning.
type PaginatedResult struct {
	Items   []json.RawMessage
	HasMore bool
	Credits CreditMeta
}

// Paginate fetches records from a paginated API endpoint, following cursors
// until the requested limit is reached or no more results exist. Returns
// the full result or an error -- partial results are never exposed.
func (c *Client) Paginate(ctx context.Context, path string, query map[string]string, lc LimitConfig) (*PaginatedResult, error) {
	effective := lc.EffectiveLimit()
	if effective > MaxCeiling {
		effective = MaxCeiling
	}
	collected := make([]json.RawMessage, 0, min(effective, apiPageSizeMax))

	// Clone query map to avoid mutating the caller's map.
	q := make(map[string]string, len(query)+2)
	for k, v := range query {
		q[k] = v
	}

	var lastCredits CreditMeta

	for {
		remaining := effective - len(collected)
		if remaining <= 0 {
			break
		}

		pageSize := min(remaining, apiPageSizeMax)
		q["size"] = strconv.Itoa(pageSize)

		resp, err := c.Get(ctx, path, q)
		if err != nil {
			return nil, err
		}
		lastCredits = resp.Credits

		var page pageResponse
		if err := json.Unmarshal(resp.Body, &page); err != nil {
			return nil, &APIError{
				Message:   fmt.Sprintf("failed to parse paginated response: %v", err),
				ExitCode:  ExitClientError,
				ErrorType: ErrorTypeClient,
			}
		}

		collected = append(collected, page.Items...)

		// No more pages available from the API.
		if page.NextCursor == nil || *page.NextCursor == "" {
			return &PaginatedResult{
				Items:   collected,
				HasMore: false,
				Credits: lastCredits,
			}, nil
		}

		// Reached the requested limit with more pages available.
		if len(collected) >= effective {
			return &PaginatedResult{
				Items:   collected[:effective],
				HasMore: true,
				Credits: lastCredits,
			}, nil
		}

		q["cursor"] = *page.NextCursor
	}

	// This branch handles the case where effective was already 0 or
	// we broke out of the loop after collecting enough items.
	return &PaginatedResult{
		Items:   collected,
		HasMore: len(collected) >= effective && effective > 0,
		Credits: lastCredits,
	}, nil
}
