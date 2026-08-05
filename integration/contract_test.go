//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

// contractCanaryTag is a value the canonical tag vocabulary will never contain.
// It is deliberately unmistakable so that if the API ever stops rejecting it,
// the failure reads as "vocabulary validation regressed" rather than "someone
// added a tag with an unlucky name".
const contractCanaryTag = "__shovels_contract_canary_invalid_tag_v1__"

// envelope is the CLI's documented output shape: {"data": [...], "meta": {...}}.
type envelope struct {
	Data []json.RawMessage `json:"data"`
	Meta map[string]any    `json:"meta"`
}

// TestRepeatedKeyFiltersAreAcceptedLive covers the exact break that hit v0.8.0.
//
// /properties/search moved permit_tags, permit_status and permit_tags_unfinaled
// to repeated keys and began rejecting a comma-joined value with a 422. The
// shipped CLI was sending the comma form its own --help documented, so every
// invocation following the help text failed. No stubbed test could catch it:
// the CLI was sending exactly what the CLI believed it should.
//
// Each case passes comma-separated input, which pflag splits into repeated
// keys. That is the contract under test — that the CLI's encoding is still
// accepted — so an empty data array passes. Whether both values reached the
// wire is already pinned deterministically in cmd/properties_test.go; asserting
// row counts here would turn a transport test into a volatile data test.
func TestRepeatedKeyFiltersAreAcceptedLive(t *testing.T) {
	for _, tc := range []struct {
		flag  string
		value string
	}{
		{"--permit-tags", "solar,roofing"},
		{"--permit-status", "final,active"},
		{"--permit-tags-unfinaled", "solar,roofing"},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			result := runCLI(t,
				"properties", "search",
				"--geo-id", "92024",
				tc.flag, tc.value,
				"--limit", "1",
			)

			if result.ExitCode != 0 {
				t.Fatalf("%s %q rejected by the live API: exit %d\nstderr: %s",
					tc.flag, tc.value, result.ExitCode, result.Stderr)
			}
			if strings.TrimSpace(result.Stderr) != "" {
				t.Errorf("expected empty stderr on success, got: %s", result.Stderr)
			}

			var env envelope
			if err := json.Unmarshal([]byte(result.Stdout), &env); err != nil {
				t.Fatalf("stdout is not a JSON envelope: %v\nstdout: %s", err, result.Stdout)
			}
			if env.Data == nil {
				t.Error("envelope has no data array")
			}
			if env.Meta == nil {
				t.Error("envelope has no meta object")
			}
		})
	}
}

// TestUnknownTagIsRejectedNamingTheValue is the one case that discriminates a
// value-dropping CLI from a working one.
//
// "Repeated keys return 200" proves nothing: two valid values still return 200
// when the CLI silently drops one, which is precisely the ENG-4040 bug. The
// sentinel goes in the MIDDLE, between two valid exclusions, so that dropping
// values in either direction leaves a request the API happily answers:
//
//   - a CLI that sends all three -> the API sees the sentinel -> 422 naming it
//   - last-value-wins            -> sends only "-roofing"     -> 200, test fails
//   - first-value-wins           -> sends only "-solar"       -> 200, test fails
//
// Verified against production: all three forms behave exactly as above. An
// unknown-first/valid-last construction would only catch last-value-wins, which
// happens to be Cobra's behaviour but is not the only way a client drops input.
//
// The two valid values are exclusions on purpose. Absence queries are where
// silent dropping does the most damage — losing an exclusion WIDENS the result
// set into a longer list that still passes every sanity check — so that is the
// path worth exercising live.
//
// What it proves precisely is that the MIDDLE value reached the wire — a client
// that comma-joined all three into one value would also produce a 422 naming
// the canary as a substring. The positive cases above cover that half, so the
// suite as a whole is sound; this case alone is not self-sufficient.
//
// It also covers live vocabulary validation and the CLI's translation of a
// structured API error, and costs nothing: a 422 never reaches billing
// (measured).
func TestUnknownTagIsRejectedNamingTheValue(t *testing.T) {
	result := runCLI(t,
		"properties", "search",
		"--geo-id", "92024",
		"--permit-tags=-solar",
		"--permit-tags="+contractCanaryTag,
		"--permit-tags=-roofing",
		"--limit", "1",
	)

	if result.ExitCode == 0 {
		t.Fatalf("expected a non-zero exit for an unknown tag, got 0.\n"+
			"Either the API stopped validating the tag vocabulary, or the CLI "+
			"dropped the middle value and sent only a valid exclusion — the "+
			"ENG-4040 failure mode.\nstdout: %s", result.Stdout)
	}
	if strings.TrimSpace(result.Stdout) != "" {
		t.Errorf("stdout must stay empty on error so it is always parseable, got: %s", result.Stdout)
	}
	if !strings.Contains(result.Stderr, contractCanaryTag) {
		t.Errorf("the error must name the offending value, otherwise a caller cannot tell "+
			"which of several tags was wrong.\nstderr: %s", result.Stderr)
	}
	if !strings.Contains(result.Stderr, "validation_error") {
		t.Errorf("expected a structured validation_error on stderr, got: %s", result.Stderr)
	}
}

// TestSiblingSearchesAcceptRepeatedTagsLive covers the two commands that carry
// the most traffic. Only /properties/search had a live request before, so an
// API-side tightening on /permits/search or /contractors/search — the same
// class of event that broke v0.8.0 — would have been caught by nothing here.
//
// Both commands already send --tags and --status as repeated keys, so this
// pins that encoding rather than a comma-joined one. Two more searches, two
// more credits a night, against a documented budget of six.
func TestSiblingSearchesAcceptRepeatedTagsLive(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{
			name: "permits search",
			args: []string{"permits", "search",
				"--geo-id", "92024",
				"--permit-from", "2024-01-01", "--permit-to", "2024-12-31",
				"--tags", "solar,roofing",
				"--limit", "1"},
		},
		{
			name: "contractors search",
			args: []string{"contractors", "search",
				"--geo-id", "92024",
				"--permit-from", "2024-01-01", "--permit-to", "2024-12-31",
				"--tags", "solar,roofing",
				"--limit", "1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := runCLI(t, tc.args...)

			if result.ExitCode != 0 {
				t.Fatalf("%s rejected by the live API: exit %d\nstderr: %s",
					tc.name, result.ExitCode, result.Stderr)
			}

			var env envelope
			if err := json.Unmarshal([]byte(result.Stdout), &env); err != nil {
				t.Fatalf("stdout is not a JSON envelope: %v\nstdout: %s", err, result.Stdout)
			}
			if env.Data == nil {
				t.Error("envelope has no data array")
			}
			if env.Meta == nil {
				t.Error("envelope has no meta object")
			}
		})
	}
}
