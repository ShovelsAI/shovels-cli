//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// cappedSearch names one of the searches whose endpoint returns at most a fixed
// number of records and no cursor.
type cappedSearch struct {
	name string
	args []string
	cap  int
}

// cappedSearches lists every capped search with the cap its endpoint enforces.
// The caps are the values a live query matching more rows than the cap returned:
// /addresses/search served 20 for q="Main St" and the three geo searches served
// 15 for q="San", q="Wa" and q="City", each with next_cursor null.
func cappedSearches() []cappedSearch {
	return []cappedSearch{
		{name: "addresses search", args: []string{"addresses", "search", "-q", "Main St"}, cap: 20},
		{name: "cities search", args: []string{"cities", "search", "-q", "San"}, cap: 15},
		{name: "counties search", args: []string{"counties", "search", "-q", "Wa"}, cap: 15},
		{name: "jurisdictions search", args: []string{"jurisdictions", "search", "-q", "City"}, cap: 15},
	}
}

// makeCappedSearchHandler mimics a server-capped search endpoint: it ignores
// size and serves one response per entry of pageCounts, cursoring only where a
// later page follows. A single count is the capped endpoint as measured; more
// than one stands in for the API change a recorded cap must not outlive.
func makeCappedSearchHandler(pageCounts ...int) (http.Handler, *[]map[string][]string) {
	var served atomic.Int32
	capturedQueries := &[]map[string][]string{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := map[string][]string{}
		for k, v := range r.URL.Query() {
			params[k] = v
		}
		*capturedQueries = append(*capturedQueries, params)

		page := int(served.Add(1)) - 1
		if page >= len(pageCounts) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"detail":"fetched past the last page"}`))
			return
		}

		items := make([]json.RawMessage, pageCounts[page])
		for i := range pageCounts[page] {
			items[i] = json.RawMessage(fmt.Sprintf(
				`{"geo_id":"geo_%05d","name":"PLACE %d","state":"ST"}`, i, i,
			))
		}

		var nextCursor *string
		if page+1 < len(pageCounts) {
			c := fmt.Sprintf("cursor_page_%d", page+2)
			nextCursor = &c
		}

		w.Header().Set("X-Credits-Request", "1")
		w.Header().Set("X-Credits-Remaining", "9999")

		resp := struct {
			Items      []json.RawMessage `json:"items"`
			NextCursor *string           `json:"next_cursor"`
		}{Items: items, NextCursor: nextCursor}
		json.NewEncoder(w).Encode(resp)
	})

	return handler, capturedQueries
}

// dataLen returns the number of records in the envelope's data array.
func dataLen(t *testing.T, parsed envelope) int {
	t.Helper()
	var data []json.RawMessage
	if err := json.Unmarshal(parsed.Data, &data); err != nil {
		t.Fatalf("expected data array: %v", err)
	}
	return len(data)
}

// serverCap returns the disclosed cap, failing when the envelope carries none.
func serverCap(t *testing.T, parsed envelope) int {
	t.Helper()
	disclosed, ok := parsed.Meta["server_capped"]
	if !ok {
		t.Fatalf("expected meta.server_capped, got meta %v", parsed.Meta)
	}
	return int(disclosed.(float64))
}

// --- Happy paths ---

func TestCappedSearchDisclosesCapAndSendsNoPagination(t *testing.T) {
	for _, search := range cappedSearches() {
		t.Run(search.name, func(t *testing.T) {
			handler, queries := makeCappedSearchHandler(search.cap)
			srv := httptest.NewServer(handler)
			defer srv.Close()

			env := withIsolatedConfig(t)
			args := append([]string{"--base-url", srv.URL}, search.args...)
			result := runCLIWithEnv(t, env, append(args, "--limit", "2")...)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			parsed := parseEnvelope(t, result.Stdout)

			if got := dataLen(t, parsed); got != 2 {
				t.Errorf("expected 2 records, got %d", got)
			}
			if got := parsed.Meta["has_more"].(bool); got {
				t.Error("expected has_more=false: the endpoint returned every record it has")
			}
			if got := serverCap(t, parsed); got != search.cap {
				t.Errorf("expected server_capped=%d, got %d", search.cap, got)
			}

			if len(*queries) != 1 {
				t.Fatalf("expected 1 request, got %d", len(*queries))
			}
			sent := (*queries)[0]
			if _, ok := sent["size"]; ok {
				t.Errorf("expected no size parameter, got %v", sent["size"])
			}
			if _, ok := sent["cursor"]; ok {
				t.Errorf("expected no cursor parameter, got %v", sent["cursor"])
			}
			if len(sent["q"]) != 1 {
				t.Errorf("expected the search term to be sent, got params %v", sent)
			}
		})
	}
}

func TestPermitsSearchKeepsCursorPagination(t *testing.T) {
	handler, queries := makePermitSearchHandler(200, 1, 9999)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"permits", "search",
		"--geo-id", "92024",
		"--permit-from", "2024-01-01",
		"--permit-to", "2024-12-31",
		"--limit", "10",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	if got := dataLen(t, parsed); got != 10 {
		t.Errorf("expected 10 records, got %d", got)
	}
	if got := parsed.Meta["has_more"].(bool); !got {
		t.Error("expected has_more=true: the endpoint has 200 records behind a cursor")
	}
	if _, ok := parsed.Meta["server_capped"]; ok {
		t.Errorf("expected no server_capped on a cursor-paginated search, got meta %v", parsed.Meta)
	}

	if len(*queries) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*queries))
	}
	if got := (*queries)[0]["size"]; len(got) != 1 || got[0] != "10" {
		t.Errorf("expected size=10 to be sent, got %v", got)
	}
}

// --- Edge cases ---

func TestCappedSearchLimitAllStopsAtCap(t *testing.T) {
	handler, _ := makeCappedSearchHandler(15)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "search", "-q", "San",
		"--limit", "all",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	if got := dataLen(t, parsed); got != 15 {
		t.Errorf("expected the cap's worth of records, got %d", got)
	}
	if got := parsed.Meta["has_more"].(bool); got {
		t.Error("expected has_more=false: --limit all already exhausted the endpoint")
	}
	if got := serverCap(t, parsed); got != 15 {
		t.Errorf("expected server_capped=15, got %d", got)
	}
}

func TestCappedSearchMaxRecordsBoundsBelowCap(t *testing.T) {
	handler, _ := makeCappedSearchHandler(15)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "search", "-q", "San",
		"--limit", "all",
		"--max-records", "4",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	if got := dataLen(t, parsed); got != 4 {
		t.Errorf("expected --max-records to bound the result to 4, got %d", got)
	}
}

func TestCappedSearchDryRunOmitsPagination(t *testing.T) {
	for _, search := range cappedSearches() {
		t.Run(search.name, func(t *testing.T) {
			env := withIsolatedConfig(t)
			result := runCLIWithEnv(t, env, append(search.args, "--dry-run")...)

			if result.ExitCode != 0 {
				t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
			}

			out := parseDryRun(t, result.Stdout)

			endpoint := "/" + strings.Fields(search.name)[0] + "/search"
			if !strings.HasSuffix(out.URL, endpoint) {
				t.Errorf("expected URL ending with %s, got %q", endpoint, out.URL)
			}
			if out.Params["q"] == nil {
				t.Errorf("expected the search term in the previewed params, got %v", out.Params)
			}
			if _, ok := out.Params["size"]; ok {
				t.Errorf("expected no size in the previewed params, got %v", out.Params["size"])
			}
			if _, ok := out.Params["cursor"]; ok {
				t.Errorf("expected no cursor in the previewed params, got %v", out.Params["cursor"])
			}
		})
	}
}

// --- Error conditions ---

func TestCappedSearchReportsCursorInsteadOfCap(t *testing.T) {
	handler, _ := makeCappedSearchHandler(15, 5)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "search", "-q", "San",
		"--limit", "15",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	if got := parsed.Meta["has_more"].(bool); !got {
		t.Error("expected has_more=true from the cursor the endpoint returned")
	}
	if _, ok := parsed.Meta["server_capped"]; ok {
		t.Errorf("expected no server_capped once the endpoint returned a cursor, got meta %v", parsed.Meta)
	}
}

func TestCappedSearchFollowsUnexpectedCursor(t *testing.T) {
	handler, queries := makeCappedSearchHandler(15, 5)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "search", "-q", "San",
		"--limit", "20",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// 20 records is past the recorded cap of 15, so the second page is fetched
	// rather than the cap assumed.
	if got := dataLen(t, parsed); got != 20 {
		t.Errorf("expected 20 records across both pages, got %d", got)
	}
	if len(*queries) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(*queries))
	}
	if got := (*queries)[1]["cursor"]; len(got) != 1 || got[0] != "cursor_page_2" {
		t.Errorf("expected the second request to carry the cursor, got %v", got)
	}

	// The envelope describes the pagination the caller got: the cursor the
	// first page carried disproves the cap for the whole result, and the second
	// page's absent cursor is what ends it.
	if _, ok := parsed.Meta["server_capped"]; ok {
		t.Errorf("expected no server_capped after following a cursor, got meta %v", parsed.Meta)
	}
	if got := parsed.Meta["has_more"].(bool); got {
		t.Error("expected has_more=false from the second page's absent cursor")
	}
}

// --- Boundary conditions ---

func TestCappedSearchLimitEqualToCapReportsNoTruncation(t *testing.T) {
	handler, _ := makeCappedSearchHandler(15)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "search", "-q", "San",
		"--limit", "15",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	if got := dataLen(t, parsed); got != 15 {
		t.Errorf("expected 15 records, got %d", got)
	}
	if got := parsed.Meta["has_more"].(bool); got {
		t.Error("expected has_more=false when --limit equals the cap")
	}
}

func TestCappedSearchDisclosesCapOnShortResult(t *testing.T) {
	handler, _ := makeCappedSearchHandler(3)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	env := withIsolatedConfig(t)
	result := runCLIWithEnv(t, env,
		"--base-url", srv.URL,
		"cities", "search", "-q", "San",
	)

	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	parsed := parseEnvelope(t, result.Stdout)

	// The disclosure describes the endpoint, so a result nowhere near the cap
	// still carries it.
	if got := dataLen(t, parsed); got != 3 {
		t.Errorf("expected 3 records, got %d", got)
	}
	if got := serverCap(t, parsed); got != 15 {
		t.Errorf("expected server_capped=15, got %d", got)
	}
}
