package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds the deploy configuration stored in ~/.friendo/config.
type Config struct {
	Token   string              `json:"token"`           // Platform session token (Better Auth)
	BaseURL string              `json:"base_url"`        // Platform API base URL
	Sites   map[string]SiteAuth `json:"sites,omitempty"` // cached site sessions, keyed by site URL
	path    string              // file path (not serialized)
}

// SiteAuth is a cached site admin session for a deployed site.
type SiteAuth struct {
	Email  string `json:"email"`
	Cookie string `json:"cookie"` // friendo_session value
}

// SiteAuth returns the cached session for a site target, if any.
func (c *Config) SiteAuth(target string) (SiteAuth, bool) {
	a, ok := c.Sites[target]
	return a, ok
}

// SetSiteAuth caches a site admin session for a target.
func (c *Config) SetSiteAuth(target, email, cookie string) {
	if c.Sites == nil {
		c.Sites = map[string]SiteAuth{}
	}
	c.Sites[target] = SiteAuth{Email: email, Cookie: cookie}
}

// ClearSiteAuth removes a cached site session (e.g. after it expires).
func (c *Config) ClearSiteAuth(target string) {
	delete(c.Sites, target)
}

// Logout clears the cached platform token and all site sessions from disk.
func Logout() error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	hadToken := cfg.Token != "" || len(cfg.Sites) > 0
	cfg.Token = ""
	cfg.Sites = nil
	if err := cfg.Save(); err != nil {
		return err
	}
	if !hadToken {
		fmt.Println("Not logged in.")
	}
	return nil
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
