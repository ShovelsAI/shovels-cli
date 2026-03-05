//go:build eval

// Package evals runs LLM usability tests against the shovels CLI.
// Each scenario gives an LLM agent a natural-language task and only
// access to the binary's --help output. The agent must discover the
// right commands, flags, and workflows to produce a valid result.
//
// Prerequisites:
//   - claude CLI in PATH
//   - SHOVELS_API_KEY environment variable
//
// Run:
//
//	go test -tags=eval ./evals/... -v -timeout 10m
package evals

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// binaryPath holds the absolute path to the compiled shovels binary,
// built once in TestMain and reused by all eval tests.
var binaryPath string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "shovels-eval-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	binaryPath = filepath.Join(tmpDir, "shovels")

	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = moduleRoot()
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "binary build failed: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(tmpDir)
	os.Exit(code)
}

func moduleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(fmt.Sprintf("cannot determine working directory: %v", err))
	}
	return filepath.Dir(dir)
}

// AgentReport is the structured response from the LLM agent.
type AgentReport struct {
	Steps []struct {
		Command string `json:"command"`
		Purpose string `json:"purpose"`
	} `json:"steps"`
	FinalCommand    string   `json:"final_command"`
	FinalOutput     string   `json:"final_output"`
	Success         bool     `json:"success"`
	UsabilityRating int      `json:"usability_rating"`
	UsabilityNotes  string   `json:"usability_notes"`
	Issues          []string `json:"issues"`
}

func requireClaude(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not found in PATH")
	}
}

func requireAPIKey(t *testing.T) {
	t.Helper()
	if os.Getenv("SHOVELS_API_KEY") == "" {
		t.Skip("SHOVELS_API_KEY not set")
	}
}

const systemPromptTmpl = `You are testing a CLI tool called "shovels". You have never used this tool before. Your only resource is the tool's --help output.

The binary is at: %s

Rules:
1. Start by running the binary with --help to understand what it does.
2. Use --help on subcommands to discover flags and required arguments.
3. Do NOT guess ID formats. If you need an ID for a city, county, or jurisdiction, use the appropriate search command to resolve it first.
4. The CLI requires SHOVELS_API_KEY to be set (it is already configured).
5. All CLI output is JSON. Parse it to verify your results.
6. After completing the task, output EXACTLY one JSON object with this schema (no other text after it):

{"steps": [{"command": "...", "purpose": "..."}], "final_command": "the command that produced the answer", "final_output": "the complete raw stdout from that command", "success": true, "usability_rating": 5, "usability_notes": "what was clear or unclear", "issues": []}

The usability_rating is 1-5 where 5 means the help text made the task trivial.
Output the JSON report as the very last thing you write.`

func runAgent(t *testing.T, scenario Scenario) AgentReport {
	t.Helper()

	prompt := fmt.Sprintf(systemPromptTmpl, binaryPath)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "claude",
		"--print",
		"--output-format", "text",
		"--allowedTools", "Bash",
		"--system-prompt", prompt,
		"--max-budget-usd", "1.00",
		scenario.Task,
	)

	// Strip CLAUDECODE env var so claude CLI doesn't refuse to run
	// inside an existing Claude Code session (e.g. when running evals
	// from within Claude Code).
	env := os.Environ()
	filtered := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(e, "CLAUDECODE=") {
			filtered = append(filtered, e)
		}
	}
	cmd.Env = filtered

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	t.Logf("scenario %s completed in %s", scenario.Name, elapsed)

	if err != nil {
		t.Logf("claude stderr: %s", stderr.String())
		t.Fatalf("claude CLI failed: %v", err)
	}

	raw := stdout.String()
	report := extractJSON(t, raw)

	t.Logf("steps taken: %d", len(report.Steps))
	for i, s := range report.Steps {
		t.Logf("  step %d: %s — %s", i+1, s.Command, s.Purpose)
	}
	t.Logf("final command: %s", report.FinalCommand)
	t.Logf("usability: %d/5 — %s", report.UsabilityRating, report.UsabilityNotes)
	if len(report.Issues) > 0 {
		t.Logf("issues: %s", strings.Join(report.Issues, "; "))
	}

	return report
}

// extractJSON finds the last complete JSON object in the agent's output.
func extractJSON(t *testing.T, raw string) AgentReport {
	t.Helper()

	// Walk backwards to find the last '{' that starts a balanced object.
	for i := len(raw) - 1; i >= 0; i-- {
		if raw[i] != '{' {
			continue
		}
		candidate := raw[i:]
		// Find the matching close brace.
		depth := 0
		end := -1
		inString := false
		escape := false
		for j, ch := range candidate {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' && inString {
				escape = true
				continue
			}
			if ch == '"' {
				inString = !inString
				continue
			}
			if inString {
				continue
			}
			switch ch {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					end = j + 1
				}
			}
			if end != -1 {
				break
			}
		}
		if end == -1 {
			continue
		}

		var report AgentReport
		if err := json.Unmarshal([]byte(candidate[:end]), &report); err != nil {
			continue // not our target object, keep looking
		}
		// Verify it looks like a report (has usability_rating).
		if report.UsabilityRating > 0 {
			return report
		}
	}

	t.Fatalf("no valid agent report found in output:\n%s", raw)
	return AgentReport{} // unreachable
}

func TestEval(t *testing.T) {
	requireAPIKey(t)
	requireClaude(t)

	for _, sc := range scenarios {
		t.Run(sc.Name, func(t *testing.T) {
			report := runAgent(t, sc)

			// --- Hard assertions ---

			if !report.Success {
				t.Error("agent reported failure")
			}

			if report.FinalOutput == "" {
				t.Fatal("agent returned empty final_output")
			}

			// Scenarios with custom validators handle their own
			// output parsing (schema, dry-run, jq pipelines).
			if sc.ValidateOutput != nil {
				sc.ValidateOutput(t, report)
			} else {
				// Default validation for standard CLI envelope
				// responses (data array + meta).
				parsed := extractJSONObject(t, report.FinalOutput)

				for _, field := range sc.MustHaveFields {
					parts := strings.Split(field, ".")
					if !hasNestedField(parsed, parts) {
						t.Errorf("final_output missing required field %q", field)
					}
				}

				if sc.MinResults > 0 {
					data, ok := parsed["data"].([]any)
					if !ok {
						t.Fatal("final_output has no data array")
					}
					if len(data) < sc.MinResults {
						t.Errorf("expected >= %d results, got %d", sc.MinResults, len(data))
					}
				}

				// Check domain correctness: verify the agent queried
				// the right resource by looking for domain-specific
				// fields. Permits have "number"; contractors have
				// "license".
				if sc.Domain != "" {
					if data, ok := parsed["data"].([]any); ok && len(data) > 0 {
						if first, ok := data[0].(map[string]any); ok {
							switch sc.Domain {
							case "permits":
								if _, ok := first["number"]; !ok {
									t.Error("expected permits result (field 'number'), got something else")
								}
							case "contractors":
								if _, ok := first["license"]; !ok {
									t.Error("expected contractors result (field 'license'), got something else")
								}
							}
						}
					}
				}
			}

			// --- Usability rating gate ---

			if report.UsabilityRating < 4 {
				if sc.EnforceUsability {
					t.Errorf("usability rating %d/5 is below required minimum of 4: %s",
						report.UsabilityRating, report.UsabilityNotes)
				} else {
					t.Logf("WARNING: low usability rating %d/5: %s",
						report.UsabilityRating, report.UsabilityNotes)
				}
			}
		})
	}
}

// extractJSONObject tries to parse raw as JSON directly; if that fails,
// it scans for the first { that starts a valid JSON object containing a
// "data" key (to distinguish CLI output from other JSON in the text).
func extractJSONObject(t *testing.T, raw string) map[string]any {
	t.Helper()

	// Fast path: raw is already valid JSON.
	var direct map[string]any
	if err := json.Unmarshal([]byte(raw), &direct); err == nil {
		return direct
	}

	// Slow path: scan for first JSON object with a "data" key.
	for i := 0; i < len(raw); i++ {
		if raw[i] != '{' {
			continue
		}
		// Use json.Decoder to find the extent of the object.
		dec := json.NewDecoder(strings.NewReader(raw[i:]))
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			continue
		}
		if _, ok := obj["data"]; ok {
			return obj
		}
	}

	t.Fatalf("no valid JSON object with 'data' key found in final_output:\n%.500s", raw)
	return nil
}

func hasNestedField(m map[string]any, path []string) bool {
	if len(path) == 0 {
		return true
	}
	val, ok := m[path[0]]
	if !ok {
		return false
	}
	if len(path) == 1 {
		return true
	}
	nested, ok := val.(map[string]any)
	if !ok {
		return false
	}
	return hasNestedField(nested, path[1:])
}
