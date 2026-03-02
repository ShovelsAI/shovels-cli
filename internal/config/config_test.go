package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTempConfigDir sets XDG_CONFIG_HOME to a temp directory for the duration
// of the test and returns the temp directory path.
func withTempConfigDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	return tmpDir
}

func TestMaskAPIKeyEmpty(t *testing.T) {
	if got := MaskAPIKey(""); got != "" {
		t.Errorf("MaskAPIKey(\"\") = %q, want \"\"", got)
	}
}

func TestMaskAPIKeyShort(t *testing.T) {
	if got := MaskAPIKey("abc"); got != "***" {
		t.Errorf("MaskAPIKey(\"abc\") = %q, want \"***\"", got)
	}
}

func TestMaskAPIKeyExactly8(t *testing.T) {
	got := MaskAPIKey("sk-1abcd")
	if got != "sk-1***abcd" {
		t.Errorf("MaskAPIKey(\"sk-1abcd\") = %q, want \"sk-1***abcd\"", got)
	}
}

func TestMaskAPIKeyLong(t *testing.T) {
	got := MaskAPIKey("sk-test-1234-5678-abcd")
	if got != "sk-t***abcd" {
		t.Errorf("MaskAPIKey(\"sk-test-1234-5678-abcd\") = %q, want \"sk-t***abcd\"", got)
	}
}

func TestLoadFromFileNotExist(t *testing.T) {
	withTempConfigDir(t)

	cfg, err := LoadFromFile()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIKey != "" {
		t.Errorf("expected empty APIKey, got %q", cfg.APIKey)
	}
}

func TestLoadFromFileCorruptYAML(t *testing.T) {
	tmpDir := withTempConfigDir(t)

	dir := filepath.Join(tmpDir, configDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	corrupt := []byte(":\n  :\n    - [\ninvalid yaml {{{\n")
	if err := os.WriteFile(filepath.Join(dir, configFileName), corrupt, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err := LoadFromFile()
	if err == nil {
		t.Fatal("expected error for corrupt YAML, got nil")
	}
	if !strings.Contains(err.Error(), "parsing config") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestSaveAndLoadAPIKey(t *testing.T) {
	withTempConfigDir(t)

	if err := SaveToFile("api_key", "sk-roundtrip"); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	cfg, err := LoadFromFile()
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}
	if cfg.APIKey != "sk-roundtrip" {
		t.Errorf("expected api_key %q, got %q", "sk-roundtrip", cfg.APIKey)
	}
}

func TestSavePreservesOtherKeys(t *testing.T) {
	tmpDir := withTempConfigDir(t)

	// Write an initial config with a custom key.
	dir := filepath.Join(tmpDir, configDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initial := []byte("custom_key: hello\napi_key: old-key\n")
	if err := os.WriteFile(filepath.Join(dir, configFileName), initial, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Update only api_key.
	if err := SaveToFile("api_key", "new-key"); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	// Read back raw to verify custom_key is preserved.
	data, err := os.ReadFile(filepath.Join(dir, configFileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "custom_key") {
		t.Error("custom_key was not preserved after SaveToFile")
	}
	if !strings.Contains(content, "new-key") {
		t.Error("api_key was not updated to new-key")
	}
}

func TestSaveCorruptYAMLReturnsError(t *testing.T) {
	tmpDir := withTempConfigDir(t)

	// Write a corrupt YAML file that cannot be parsed.
	dir := filepath.Join(tmpDir, configDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	corrupt := []byte(":\n  :\n    - [\ninvalid yaml {{{\n")
	if err := os.WriteFile(filepath.Join(dir, configFileName), corrupt, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := SaveToFile("api_key", "sk-new")
	if err == nil {
		t.Fatal("expected error for corrupt YAML, got nil")
	}
	if !strings.Contains(err.Error(), "parsing existing config") {
		t.Errorf("expected parse error, got: %v", err)
	}

	// Verify the corrupt file was NOT overwritten.
	data, readErr := os.ReadFile(filepath.Join(dir, configFileName))
	if readErr != nil {
		t.Fatalf("read: %v", readErr)
	}
	if string(data) != string(corrupt) {
		t.Error("corrupt config file was overwritten despite parse error")
	}
}

func TestSaveCreatesDirectoryAndFile(t *testing.T) {
	withTempConfigDir(t)

	if err := SaveToFile("api_key", "sk-new"); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	path, err := ConfigFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("config file was not created")
	}
}

func TestResolveDefaultValues(t *testing.T) {
	withTempConfigDir(t)
	t.Setenv("SHOVELS_API_KEY", "")

	cfg, err := Resolve(Overrides{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BaseURL != DefaultBaseURL {
		t.Errorf("expected base URL %q, got %q", DefaultBaseURL, cfg.BaseURL)
	}
	if cfg.MaxLimit != DefaultLimit {
		t.Errorf("expected max limit %d, got %d", DefaultLimit, cfg.MaxLimit)
	}
}

func TestResolveEnvOverridesFileAPIKey(t *testing.T) {
	withTempConfigDir(t)
	t.Setenv("SHOVELS_API_KEY", "sk-env")

	// Write a config file with a different key.
	if err := SaveToFile("api_key", "sk-file"); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	cfg, err := Resolve(Overrides{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIKey != "sk-env" {
		t.Errorf("expected APIKey %q, got %q", "sk-env", cfg.APIKey)
	}
}

func TestResolveEnvOverridesFile(t *testing.T) {
	withTempConfigDir(t)
	t.Setenv("SHOVELS_API_KEY", "sk-env")

	if err := SaveToFile("api_key", "sk-file"); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	cfg, err := Resolve(Overrides{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIKey != "sk-env" {
		t.Errorf("expected APIKey %q, got %q", "sk-env", cfg.APIKey)
	}
}

func TestResolveFileUsedWhenNoFlagOrEnv(t *testing.T) {
	withTempConfigDir(t)
	t.Setenv("SHOVELS_API_KEY", "")

	if err := SaveToFile("api_key", "sk-file"); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	cfg, err := Resolve(Overrides{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIKey != "sk-file" {
		t.Errorf("expected APIKey %q, got %q", "sk-file", cfg.APIKey)
	}
}

func TestResolveBaseURLFlagOverridesDefault(t *testing.T) {
	withTempConfigDir(t)
	t.Setenv("SHOVELS_API_KEY", "")

	cfg, err := Resolve(Overrides{
		BaseURL:    "https://custom.api/v1",
		BaseURLSet: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BaseURL != "https://custom.api/v1" {
		t.Errorf("expected BaseURL %q, got %q", "https://custom.api/v1", cfg.BaseURL)
	}
}

func TestResolveBaseURLFileOverridesDefault(t *testing.T) {
	withTempConfigDir(t)
	t.Setenv("SHOVELS_API_KEY", "")

	if err := SaveToFile("base_url", "https://staging.api/v2"); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	cfg, err := Resolve(Overrides{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BaseURL != "https://staging.api/v2" {
		t.Errorf("expected BaseURL %q, got %q", "https://staging.api/v2", cfg.BaseURL)
	}
}

func TestResolveEmptyEnvFallsToFile(t *testing.T) {
	withTempConfigDir(t)
	t.Setenv("SHOVELS_API_KEY", "")

	if err := SaveToFile("api_key", "sk-file"); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	cfg, err := Resolve(Overrides{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIKey != "sk-file" {
		t.Errorf("expected APIKey %q from file when env empty, got %q", "sk-file", cfg.APIKey)
	}
}

func TestConfigFilePath(t *testing.T) {
	withTempConfigDir(t)

	path, err := ConfigFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(path) != configFileName {
		t.Errorf("expected filename %q, got %q", configFileName, filepath.Base(path))
	}
}

func TestConfigDirErrorWhenHomeDirUnavailable(t *testing.T) {
	// Unset XDG_CONFIG_HOME so configDir falls through to os.UserHomeDir.
	t.Setenv("XDG_CONFIG_HOME", "")
	// Unset HOME so os.UserHomeDir fails.
	t.Setenv("HOME", "")

	_, err := ConfigFilePath()
	if err == nil {
		t.Fatal("expected error when HOME and XDG_CONFIG_HOME are both unset, got nil")
	}
	if !strings.Contains(err.Error(), "cannot determine config directory") {
		t.Errorf("expected 'cannot determine config directory' error, got: %v", err)
	}
}

func TestLoadFromFileErrorWhenHomeDirUnavailable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	_, err := LoadFromFile()
	if err == nil {
		t.Fatal("expected error from LoadFromFile when config dir unavailable, got nil")
	}
	if !strings.Contains(err.Error(), "cannot determine config directory") {
		t.Errorf("expected config dir error, got: %v", err)
	}
}

func TestSaveToFileErrorWhenHomeDirUnavailable(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")

	err := SaveToFile("api_key", "sk-test")
	if err == nil {
		t.Fatal("expected error from SaveToFile when config dir unavailable, got nil")
	}
	if !strings.Contains(err.Error(), "cannot determine config directory") {
		t.Errorf("expected config dir error, got: %v", err)
	}
}
