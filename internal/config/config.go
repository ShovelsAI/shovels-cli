package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultBaseURL is the production Shovels API endpoint.
	DefaultBaseURL = "https://api.shovels.ai/v2"

	// DefaultLimit is the default number of records returned.
	DefaultLimit = 50

	configDirName  = "shovels"
	configFileName = "config.yaml"
)

// Config holds resolved configuration values from all sources.
type Config struct {
	APIKey   string `yaml:"api_key"`
	BaseURL  string `yaml:"base_url,omitempty"`
	MaxLimit int    `yaml:"default_limit,omitempty"`
}

// configDir returns the XDG-compliant config directory path.
// It respects XDG_CONFIG_HOME if set, otherwise defaults to ~/.config.
func configDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, configDirName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", configDirName)
	}
	return filepath.Join(home, ".config", configDirName)
}

// ConfigFilePath returns the absolute path to the config file.
func ConfigFilePath() string {
	return filepath.Join(configDir(), configFileName)
}

// LoadFromFile reads the YAML config file. Returns zero Config if the
// file does not exist (not an error). Returns an error only if the file
// exists but cannot be read or parsed.
func LoadFromFile() (Config, error) {
	path := ConfigFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}

// SaveToFile writes the given key-value pair to the config file,
// preserving any existing keys. Creates the directory and file if needed.
func SaveToFile(key, value string) error {
	dir := configDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	path := ConfigFilePath()
	existing := make(map[string]any)

	data, err := os.ReadFile(path)
	if err == nil {
		// File exists and is readable — parse it to preserve existing keys.
		if parseErr := yaml.Unmarshal(data, &existing); parseErr != nil {
			return fmt.Errorf("parsing existing config: %w", parseErr)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("reading existing config: %w", err)
	}

	existing[key] = value

	out, err := yaml.Marshal(existing)
	if err != nil {
		return fmt.Errorf("encoding config: %w", err)
	}

	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// Overrides holds flag values that were explicitly set by the user.
// Only values where Set is true participate in the precedence chain.
type Overrides struct {
	APIKey    string
	APIKeySet bool
	BaseURL   string
	BaseURLSet bool
}

// Resolve builds a Config using the precedence chain: flag > env > file > default.
func Resolve(o Overrides) (Config, error) {
	fileCfg, err := LoadFromFile()
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		BaseURL:  DefaultBaseURL,
		MaxLimit: DefaultLimit,
	}

	// Base URL: flag > file > default
	if o.BaseURLSet {
		cfg.BaseURL = o.BaseURL
	} else if fileCfg.BaseURL != "" {
		cfg.BaseURL = fileCfg.BaseURL
	}

	// API key: flag > env > file
	switch {
	case o.APIKeySet:
		cfg.APIKey = o.APIKey
	case os.Getenv("SHOVELS_API_KEY") != "":
		cfg.APIKey = os.Getenv("SHOVELS_API_KEY")
	default:
		cfg.APIKey = fileCfg.APIKey
	}

	return cfg, nil
}

// MaskAPIKey returns a masked version of the API key for display.
// Keys shorter than 8 characters are fully masked. Otherwise shows
// the first 4 and last 4 characters with *** in between.
func MaskAPIKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) < 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + "***" + key[len(key)-4:]
}
