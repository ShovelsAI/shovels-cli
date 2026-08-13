package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/shovels-ai/shovels-cli/internal/contract"
	"github.com/spf13/pflag"
)

// paginationParams are the two query parameters that bound a collected record
// set: size caps a page, cursor asks for the one after it. Only Paginate sends
// cursor, and only Paginate or a paginated command's own --dry-run branch sends
// size, so either name in a request means the command collects a record set.
var paginationParams = []string{"size", "cursor"}

// transportOnlyInvocation is one runnable argv for a transport-only command
// together with the endpoint path its request must reach.
type transportOnlyInvocation struct {
	args []string
	path string
}

// coverageInvocation builds the argv for a geography's coverage subcommand,
// whose two date flags are required.
func coverageInvocation(geo, geoID string) transportOnlyInvocation {
	return transportOnlyInvocation{
		args: []string{geo, "coverage", geoID, "--coverage-from", "2024-01-01", "--coverage-to", "2024-12-31"},
		path: coverageEndpoint,
	}
}

// transportOnlyInvocations holds one invocation per transport-only command.
// Which commands must appear here comes from the contract records rather than
// from this map, so a command classified transport-only without an entry fails
// by name instead of going unexercised.
var transportOnlyInvocations = map[string]transportOnlyInvocation{
	"permits get":     {args: []string{"permits", "get", "PERMIT1"}, path: "/permits"},
	"properties get":  {args: []string{"properties", "get", "PROPERTY1"}, path: "/properties"},
	"decisions get":   {args: []string{"decisions", "get", "DECISION1"}, path: "/decisions"},
	"contractors get": {args: []string{"contractors", "get", "CONTRACTOR1"}, path: "/contractors"},
	"usage":           {args: []string{"usage"}, path: "/usage"},

	"cities coverage":        coverageInvocation("cities", "CITYID"),
	"counties coverage":      coverageInvocation("counties", "COUNTYID"),
	"jurisdictions coverage": coverageInvocation("jurisdictions", "JURID"),
	"states coverage":        coverageInvocation("states", "CA"),
	"zipcodes coverage":      coverageInvocation("zipcodes", "92024"),
}

// transportOnlyPaths returns the command paths whose contract record makes them
// transport-only, sorted so the cases run and report in a stable order. ModeNone
// says the command collects no record set, and honoring --timeout says it
// applies that flag to a client of its own. version reaches the API too, but its
// record honors nothing because the timeout it uses is a constant.
func transportOnlyPaths() []string {
	var paths []string
	for path, record := range contract.All() {
		if record.Mode == contract.ModeNone && record.Honors(contract.FlagTimeout) {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths
}

// recordedRequest is one request the stub server answered.
type recordedRequest struct {
	path  string
	query url.Values
}

// paginationParamsIn returns the pagination parameters the query carries, named
// in paginationParams order.
func paginationParamsIn(query url.Values) []string {
	var found []string
	for _, name := range paginationParams {
		if _, ok := query[name]; ok {
			found = append(found, name)
		}
	}
	return found
}

// resetGlobalFlags restores every persistent flag on root to its default and
// clears the record that it was set.
//
// Root registers these flags once and hands every command the same *pflag.Flag,
// so a value one Execute parsed is still in the flag on the next. Clearing
// Changed alone would leave the earlier value in force, and a case would then
// run with arguments other than the ones it names.
func resetGlobalFlags(t *testing.T) {
	t.Helper()
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if err := f.Value.Set(f.DefValue); err != nil {
			t.Fatalf("restoring --%s to its default %q: %v", f.Name, f.DefValue, err)
		}
		f.Changed = false
	})
}

// runThroughRoot executes argv against the shared command tree with --base-url
// pointed at a stub that answers every request with body, and returns the
// requests it received alongside stdout.
//
// The tree is a package-level singleton, so what a run parses and resolves is
// still in place for the next one. Every piece of that state is restored around
// the run: root's persistent flags, the args and output sink on rootCmd, and the
// config PersistentPreRunE resolves. versionCmd, which
// TestVersionExitCodeAlwaysZero invokes with a bare command of its own, reads
// that config and calls the API whenever it finds a key in it — under that
// command's nil context, which panics.
//
// A leaf's own flags survive a run too, and nothing here restores them: an
// invocation therefore passes every flag its command needs rather than leaving
// one to whatever an earlier case set.
func runThroughRoot(t *testing.T, body string, argv ...string) ([]recordedRequest, string, error) {
	t.Helper()

	var requests []recordedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, recordedRequest{path: r.URL.Path, query: r.URL.Query()})
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SHOVELS_API_KEY", "sk-test")

	previousConfig := resolvedConfig
	resetGlobalFlags(t)
	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetArgs(append([]string{"--base-url", srv.URL}, argv...))
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		resetGlobalFlags(t)
		resolvedConfig = previousConfig
	})

	err := rootCmd.Execute()

	// Closing joins the handler goroutines, so the recorded requests are
	// complete and safe to read once it returns.
	srv.Close()
	return requests, stdout.String(), err
}

// dryRunParams runs argv with --dry-run and returns the printed request's
// params as url.Values, so a dry-run rendering can be compared against a
// recorded request. Array-shaped params print as JSON arrays; the rest print as
// single scalars.
func dryRunParams(t *testing.T, argv ...string) (url.Values, string) {
	t.Helper()

	requests, stdout, err := runThroughRoot(t, "", append(slices.Clone(argv), "--dry-run")...)
	if err != nil {
		t.Fatalf("--dry-run on %v: %v", argv, err)
	}
	if len(requests) != 0 {
		t.Fatalf("--dry-run on %v issued %d requests", argv, len(requests))
	}

	var printed struct {
		URL    string         `json:"url"`
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal([]byte(stdout), &printed); err != nil {
		t.Fatalf("--dry-run output on %v is not JSON: %v\n%s", argv, err, stdout)
	}

	params := url.Values{}
	for name, value := range printed.Params {
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				params.Add(name, fmt.Sprint(item))
			}
		default:
			params.Add(name, fmt.Sprint(v))
		}
	}
	return params, printed.URL
}

// --- Happy paths ---

func TestTransportOnlyCommandsSendNoPaginationParameters(t *testing.T) {
	for _, path := range transportOnlyPaths() {
		t.Run(path, func(t *testing.T) {
			invocation, ok := transportOnlyInvocations[path]
			if !ok {
				t.Fatalf("%s is classified transport-only but no invocation exercises it", path)
			}

			requests, stdout, err := runThroughRoot(t, `{"items":[{"id":"ITEM1"}]}`, invocation.args...)

			if err != nil {
				t.Fatalf("%s failed: %v", path, err)
			}
			if len(requests) != 1 {
				t.Fatalf("%s must issue exactly 1 request, got %d", path, len(requests))
			}
			if requests[0].path != invocation.path {
				t.Errorf("%s must request %q, got %q", path, invocation.path, requests[0].path)
			}
			if found := paginationParamsIn(requests[0].query); len(found) > 0 {
				t.Errorf("%s must send no pagination parameter, got %v in %v", path, found, requests[0].query)
			}
			if !strings.Contains(stdout, `"data"`) {
				t.Errorf("%s must print an envelope, got %q", path, stdout)
			}
		})
	}
}

// --- Edge cases ---

func TestTransportOnlyDryRunMatchesTheRealRequest(t *testing.T) {
	for _, path := range transportOnlyPaths() {
		record, _ := contract.Lookup(path)
		if !record.Honors(contract.FlagDryRun) {
			continue
		}

		t.Run(path, func(t *testing.T) {
			invocation := transportOnlyInvocations[path]
			requests, _, err := runThroughRoot(t, `{"items":[{"id":"ITEM1"}]}`, invocation.args...)
			if err != nil {
				t.Fatalf("%s failed: %v", path, err)
			}
			if len(requests) != 1 {
				t.Fatalf("%s must issue exactly 1 request, got %d", path, len(requests))
			}

			params, printedURL := dryRunParams(t, invocation.args...)

			if !strings.HasSuffix(printedURL, invocation.path) {
				t.Errorf("%s --dry-run must print a URL ending in %q, got %q", path, invocation.path, printedURL)
			}
			if found := paginationParamsIn(params); len(found) > 0 {
				t.Errorf("%s --dry-run must print no pagination parameter, got %v in %v", path, found, params)
			}
			if !reflect.DeepEqual(map[string][]string(params), map[string][]string(requests[0].query)) {
				t.Errorf("%s --dry-run printed %v, real request sent %v", path, params, requests[0].query)
			}
		})
	}
}

// The transport-only assertions run through the shared command tree, so what
// they observe depends on the reset restoring the flag's value and not only its
// Changed marker. A run that inherited the previous case's --limit would send
// that value's size rather than the one its own arguments ask for.
func TestGlobalFlagResetRestoresTheDefaultValue(t *testing.T) {
	search := []string{"permits", "search", "--geo-id", "92024", "--permit-from", "2024-01-01", "--permit-to", "2024-12-31"}
	body := `{"items":[],"next_cursor":null}`

	narrowed, _, err := runThroughRoot(t, body, append(slices.Clone(search), "--limit", "7")...)
	if err != nil {
		t.Fatalf("permits search --limit 7 failed: %v", err)
	}
	restored, _, err := runThroughRoot(t, body, search...)
	if err != nil {
		t.Fatalf("permits search failed: %v", err)
	}

	if len(narrowed) != 1 || len(restored) != 1 {
		t.Fatalf("expected 1 request per run, got %d then %d", len(narrowed), len(restored))
	}
	if narrowed[0].query.Get("size") != "7" {
		t.Fatalf("--limit 7 must reach the request as size=7, got %q", narrowed[0].query.Get("size"))
	}
	defaultLimit := rootCmd.PersistentFlags().Lookup("limit").DefValue
	if got := restored[0].query.Get("size"); got != defaultLimit {
		t.Errorf("the reset must restore --limit to its default %s, got size=%q", defaultLimit, got)
	}
}

// --- Error conditions ---

// A transport-only command rewired to call Paginate emits the paginator's size,
// which is what makes TestTransportOnlyCommandsSendNoPaginationParameters a
// drift guard rather than a tautology. permits search stands in for that
// rewiring: it is a real Paginate caller, so the request it emits is the request
// the rewired command would emit.
func TestPaginationAssertionRejectsAPaginatingCommand(t *testing.T) {
	requests, _, err := runThroughRoot(t, `{"items":[],"next_cursor":null}`,
		"permits", "search", "--geo-id", "92024", "--permit-from", "2024-01-01", "--permit-to", "2024-12-31")
	if err != nil {
		t.Fatalf("permits search failed: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}

	found := paginationParamsIn(requests[0].query)

	if !slices.Contains(found, "size") {
		t.Errorf("the assertion must report size on a paginating command, found %v in %v", found, requests[0].query)
	}
}

// --- Boundary conditions ---

// The get commands fetch by ID in a single request and never reach the
// paginator, so the assertion has to cover them alongside the coverage and
// usage commands.
func TestTransportOnlyPathsIncludeTheBatchGetCommands(t *testing.T) {
	paths := transportOnlyPaths()

	for _, path := range []string{"permits get", "properties get", "decisions get", "contractors get"} {
		if !slices.Contains(paths, path) {
			t.Errorf("%s bypasses the paginator, so it must be classified transport-only; got %v", path, paths)
		}
	}
}

// TestTransportOnlyCommandsSendNoPaginationParameters iterates the records, not
// this file's table, which is what makes a missing invocation fail by name. The
// same asymmetry hides the opposite drift: a command whose record stops
// classifying it transport-only leaves its invocation here unread, and the
// pagination assertion silently covers one command fewer.
func TestEveryTransportOnlyInvocationIsClassified(t *testing.T) {
	classified := transportOnlyPaths()

	for path := range transportOnlyInvocations {
		if !slices.Contains(classified, path) {
			t.Errorf("%s has an invocation but is not classified transport-only, so nothing exercises it; classified %v", path, classified)
		}
	}
}
