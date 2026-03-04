//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withIsolatedConfig creates a temp directory for XDG_CONFIG_HOME and returns
// an env slice that overrides it, plus clears SHOVELS_API_KEY.
func withIsolatedConfig(t *testing.T) []string {
	t.Helper()
	tmpDir := t.TempDir()
	return []string{
		"XDG_CONFIG_HOME=" + tmpDir,
		"SHOVELS_API_KEY=sk-test",
	}
}

// withIsolatedConfigNoAuth is like withIsolatedConfig but clears the API key
// so commands that require auth will fail.
func withIsolatedConfigNoAuth(t *testing.T) []string {
	t.Helper()
	tmpDir := t.TempDir()
	return []string{
		"XDG_CONFIG_HOME=" + tmpDir,
		"SHOVELS_API_KEY=",
	}
}

// withIsolatedConfigDir is like withIsolatedConfig but also returns the temp dir path.
func withIsolatedConfigDir(t *testing.T) ([]string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	return []string{
		"XDG_CONFIG_HOME=" + tmpDir,
		"SHOVELS_API_KEY=sk-test",
	}, tmpDir
}

func TestConfigSetCreatesFile(t *testing.T) {
	env, tmpDir := withIsolatedConfigDir(t)

	result := runCLIWithEnv(t, env, "config", "set", "api-key", "sk-test1234abcd")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	// Verify the file was created.
	configPath := filepath.Join(tmpDir, "shovels", "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}
	if !strings.Contains(string(data), "sk-test1234abcd") {
		t.Errorf("config file does not contain the API key; content: %s", string(data))
	}
}

func TestConfigSetPreservesOtherKeys(t *testing.T) {
	env, tmpDir := withIsolatedConfigDir(t)

	// Create config dir and write a file with an extra key.
	configDir := filepath.Join(tmpDir, "shovels")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initial := "custom_field: preserved_value\napi_key: old-key\n"
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(initial), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	result := runCLIWithEnv(t, env, "config", "set", "api-key", "sk-newkey12345678")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	data, err := os.ReadFile(filepath.Join(configDir, "config.yaml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "preserved_value") {
		t.Error("custom_field was not preserved")
	}
	if !strings.Contains(content, "sk-newkey12345678") {
		t.Error("api_key was not updated")
	}
}

func TestConfigShowDefaults(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	result := runCLIWithEnv(t, env, "config", "show")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	var envelope struct {
		Data struct {
			APIKey       string `json:"api_key"`
			BaseURL      string `json:"base_url"`
			DefaultLimit int    `json:"default_limit"`
		} `json:"data"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}

	if envelope.Data.APIKey != "" {
		t.Errorf("expected empty masked api_key, got %q", envelope.Data.APIKey)
	}
	if envelope.Data.BaseURL != "https://api.shovels.ai/v2" {
		t.Errorf("expected default base_url, got %q", envelope.Data.BaseURL)
	}
	if envelope.Data.DefaultLimit != 50 {
		t.Errorf("expected default_limit 50, got %d", envelope.Data.DefaultLimit)
	}
	if envelope.Meta == nil {
		t.Error("meta field is missing")
	}
}

func TestConfigShowMasksAPIKey(t *testing.T) {
	tmpDir := t.TempDir()
	env := []string{
		"XDG_CONFIG_HOME=" + tmpDir,
		"SHOVELS_API_KEY=",
	}

	// Set an API key first.
	configDir := filepath.Join(tmpDir, "shovels")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("api_key: sk-test1234longkey\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	result := runCLIWithEnv(t, env, "config", "show")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	var envelope struct {
		Data struct {
			APIKey string `json:"api_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}

	// The key "sk-test1234longkey" should be masked to "sk-t***gkey"
	if envelope.Data.APIKey != "sk-t***gkey" {
		t.Errorf("expected masked key %q, got %q", "sk-t***gkey", envelope.Data.APIKey)
	}

	// Full key must not appear in output.
	if strings.Contains(result.Stdout, "sk-test1234longkey") {
		t.Error("full API key should not appear in config show output")
	}
}

func TestConfigShowReflectsEnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	env := []string{
		"XDG_CONFIG_HOME=" + tmpDir,
		"SHOVELS_API_KEY=sk-envvar-test1234",
	}

	result := runCLIWithEnv(t, env, "config", "show")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	var envelope struct {
		Data struct {
			APIKey string `json:"api_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}

	// "sk-envvar-test1234" masked: "sk-e***1234"
	if envelope.Data.APIKey != "sk-e***1234" {
		t.Errorf("expected masked key %q, got %q", "sk-e***1234", envelope.Data.APIKey)
	}
}

func TestConfigShowBaseURLFlagOverride(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env, "--base-url", "https://custom.api/v1", "config", "show")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	var envelope struct {
		Data struct {
			BaseURL string `json:"base_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}

	if envelope.Data.BaseURL != "https://custom.api/v1" {
		t.Errorf("expected base_url %q, got %q", "https://custom.api/v1", envelope.Data.BaseURL)
	}
}

func TestConfigSetOutputIsJSON(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env, "config", "set", "api-key", "sk-test1234abcd")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	var envelope struct {
		Data struct {
			Status string `json:"status"`
			Key    string `json:"key"`
		} `json:"data"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}

	if envelope.Data.Status != "ok" {
		t.Errorf("expected status %q, got %q", "ok", envelope.Data.Status)
	}
	if envelope.Data.Key != "api-key" {
		t.Errorf("expected key %q, got %q", "api-key", envelope.Data.Key)
	}
}

func TestNoAPIKeyOnVersionDoesNotError(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env, "version")
	if result.ExitCode != 0 {
		t.Fatalf("version should not require API key; exit %d, stderr: %s", result.ExitCode, result.Stderr)
	}
}

func TestNoAPIKeyOnHelpDoesNotError(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env, "--help")
	if result.ExitCode != 0 {
		t.Fatalf("help should not require API key; exit %d, stderr: %s", result.ExitCode, result.Stderr)
	}
}

func TestNoAPIKeyOnConfigDoesNotError(t *testing.T) {
	env := withIsolatedConfig(t)

	result := runCLIWithEnv(t, env, "config", "show")
	if result.ExitCode != 0 {
		t.Fatalf("config show should not require API key; exit %d, stderr: %s", result.ExitCode, result.Stderr)
	}
}

func TestConfigHelpListsSubcommands(t *testing.T) {
	result := runCLI(t, "config", "--help")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "set") || !strings.Contains(result.Stdout, "show") {
		t.Error("config --help should list set and show subcommands")
	}
}

func TestConfigSetNotWritableDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a read-only directory to block config file creation.
	readonlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.MkdirAll(readonlyDir, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	env := []string{
		"XDG_CONFIG_HOME=" + readonlyDir,
		"SHOVELS_API_KEY=",
	}

	result := runCLIWithEnv(t, env, "config", "set", "api-key", "sk-test")
	if result.ExitCode != 1 {
		t.Fatalf("expected exit 1 for not-writable dir, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if p.ErrorType != "client_error" {
		t.Errorf("expected error_type %q, got %q", "client_error", p.ErrorType)
	}
}

func TestConfigSetThenShowRoundTrip(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	// Set an API key.
	setResult := runCLIWithEnv(t, env, "config", "set", "api-key", "sk-roundtrip-test-1234")
	if setResult.ExitCode != 0 {
		t.Fatalf("config set failed: exit %d, stderr: %s", setResult.ExitCode, setResult.Stderr)
	}

	// Show should reflect the set key (masked).
	showResult := runCLIWithEnv(t, env, "config", "show")
	if showResult.ExitCode != 0 {
		t.Fatalf("config show failed: exit %d, stderr: %s", showResult.ExitCode, showResult.Stderr)
	}

	var envelope struct {
		Data struct {
			APIKey string `json:"api_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(showResult.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}

	// "sk-roundtrip-test-1234" masked: "sk-r***1234"
	if envelope.Data.APIKey != "sk-r***1234" {
		t.Errorf("expected masked key %q, got %q", "sk-r***1234", envelope.Data.APIKey)
	}
}

func TestConfigShowWithMalformedConfigErrors(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "shovels")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("': bad yaml\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := runCLIWithEnv(t,
		[]string{
			"XDG_CONFIG_HOME=" + tmpDir,
			"SHOVELS_API_KEY=",
		},
		"config", "show",
	)

	if result.ExitCode != 1 {
		t.Fatalf("expected exit code 1 with malformed config, got %d; stdout: %s", result.ExitCode, result.Stdout)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 1 {
		t.Errorf("expected error code 1, got %d", p.Code)
	}
	if !strings.Contains(p.Error, "parsing config") {
		t.Errorf("expected error about parsing config, got: %s", p.Error)
	}
}

// --- Auth gating tests using the _test-auth fixture command ---

func TestAuthNoAPIKeyExits2(t *testing.T) {
	env := withIsolatedConfigNoAuth(t)

	result := runCLIWithEnv(t, env, "_test-auth")
	if result.ExitCode != 2 {
		t.Fatalf("expected exit 2 with no API key, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	p := parseStderrError(t, result.Stderr)
	if p.Code != 2 {
		t.Errorf("expected error code 2, got %d", p.Code)
	}
	if p.ErrorType != "auth_error" {
		t.Errorf("expected error_type %q, got %q", "auth_error", p.ErrorType)
	}
	if !strings.Contains(p.Error, "API key not configured") {
		t.Errorf("expected error about missing API key, got: %s", p.Error)
	}
}

func TestAuthConfigFileKeyResolves(t *testing.T) {
	tmpDir := t.TempDir()
	env := []string{
		"XDG_CONFIG_HOME=" + tmpDir,
		"SHOVELS_API_KEY=",
	}

	// Write a config file with an API key.
	configDir := filepath.Join(tmpDir, "shovels")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("api_key: sk-from-file-1234\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	result := runCLIWithEnv(t, env, "_test-auth")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 with config file key, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	var envelope struct {
		Data struct {
			APIKey string `json:"api_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if envelope.Data.APIKey != "sk-from-file-1234" {
		t.Errorf("expected config file key %q, got %q", "sk-from-file-1234", envelope.Data.APIKey)
	}
}

func TestAuthEnvVarOverridesConfigFile(t *testing.T) {
	_, tmpDir := withIsolatedConfigDir(t)

	// Write a config file with an API key.
	configDir := filepath.Join(tmpDir, "shovels")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte("api_key: sk-from-file-1234\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	env := []string{
		"XDG_CONFIG_HOME=" + tmpDir,
		"SHOVELS_API_KEY=sk-from-env-5678",
	}

	result := runCLIWithEnv(t, env, "_test-auth")
	if result.ExitCode != 0 {
		t.Fatalf("expected exit 0 with env var key, got %d; stderr: %s", result.ExitCode, result.Stderr)
	}

	var envelope struct {
		Data struct {
			APIKey string `json:"api_key"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &envelope); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
	}
	if envelope.Data.APIKey != "sk-from-env-5678" {
		t.Errorf("expected env var key %q, got %q", "sk-from-env-5678", envelope.Data.APIKey)
	}
}
