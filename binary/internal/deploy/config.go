package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds the deploy configuration stored in ~/.friendo/config.
type Config struct {
	Token   string `json:"token"`             // Session token from Better Auth
	BaseURL string `json:"base_url"`          // API base URL (e.g. https://friendo.world or http://localhost:8787)
	path    string // file path (not serialized)
}

// DefaultBaseURL is the production friendo.world URL.
const DefaultBaseURL = "https://friendo.world"

// LoadConfig reads the config from ~/.friendo/config.
// Returns an empty config (no error) if the file doesn't exist yet.
func LoadConfig() (*Config, error) {
	dir, err := friendoDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, "config")
	cfg := &Config{
		BaseURL: DefaultBaseURL,
		path:    path,
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	cfg.path = path
	return cfg, nil
}

// Save writes the config to ~/.friendo/config.
func (c *Config) Save() error {
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	return os.WriteFile(c.path, data, 0o600)
}

func friendoDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".friendo"), nil
}
