package deploy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/henryholtgeerts/friendo/binary/internal/data"
)

// SiteConfig represents the friendo.toml file.
type SiteConfig struct {
	Site struct {
		Name string `toml:"name"`
	} `toml:"site"`
	Content struct {
		Types []string `toml:"types"`
	} `toml:"content"`
	Deploy struct {
		Domain string `toml:"domain"`
	} `toml:"deploy"`
}

// Options controls deploy behavior.
type Options struct {
	DryRun  bool
	BaseURL string // override API base URL (for local dev)
}

// Run executes the full deploy pipeline.
func Run(opts Options) error {
	siteDir, err := os.Getwd()
	if err != nil {
		return err
	}

	// 1. Load site config.
	siteCfg, err := loadSiteConfig(siteDir)
	if err != nil {
		return err
	}

	subdomain := sanitizeSubdomain(siteCfg.Site.Name)
	if subdomain == "" {
		subdomain = filepath.Base(siteDir)
	}

	fmt.Printf("Deploying %s to %s.friendo.world\n", siteCfg.Site.Name, subdomain)

	if opts.DryRun {
		return dryRun(siteDir, siteCfg, subdomain)
	}

	// 2. Load or create auth config.
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}

	if opts.BaseURL != "" {
		cfg.BaseURL = opts.BaseURL
	}

	// 3. Authenticate if needed.
	if cfg.Token == "" {
		fmt.Println()
		fmt.Println("You need to sign in to deploy. Opening your browser...")
		token, err := deviceAuth(cfg.BaseURL)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		cfg.Token = token
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
		fmt.Println("Authenticated successfully.")
	}

	client := NewClient(cfg.BaseURL, cfg.Token)

	// 4. Register site.
	fmt.Printf("Registering site...")
	if err := client.CreateSite(siteCfg.Site.Name, subdomain); err != nil {
		// If auth fails, clear token and tell user to retry.
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "expired") {
			cfg.Token = ""
			cfg.Save()
			return fmt.Errorf("session expired — run `friendo deploy` again to re-authenticate")
		}
		return err
	}
	fmt.Println(" done")

	// 5. Sync records.
	fmt.Printf("Syncing records...")
	records, err := readLocalRecords(siteDir)
	if err != nil {
		return err
	}
	if len(records) > 0 {
		if err := client.SyncRecords(subdomain, records); err != nil {
			return err
		}
		fmt.Printf(" %d records synced\n", len(records))
	} else {
		fmt.Println(" no records")
	}

	// 6. Upload templates and assets.
	fmt.Printf("Uploading files...")
	count, err := uploadFiles(client, siteDir, subdomain)
	if err != nil {
		return err
	}
	fmt.Printf(" %d files uploaded\n", count)

	// 7. Print result.
	fmt.Println()
	url := fmt.Sprintf("https://%s.friendo.world", subdomain)
	if siteCfg.Deploy.Domain != "" {
		url = fmt.Sprintf("https://%s", siteCfg.Deploy.Domain)
	}
	fmt.Printf("Live at %s\n", url)

	return nil
}

// --- Auth ---

func deviceAuth(baseURL string) (string, error) {
	// Generate a random device code.
	codeBytes := make([]byte, 16)
	rand.Read(codeBytes)
	code := hex.EncodeToString(codeBytes)

	// Open browser.
	authURL := fmt.Sprintf("%s/cli/auth?code=%s", baseURL, code)
	fmt.Printf("If your browser doesn't open, visit:\n  %s\n\n", authURL)
	openBrowser(authURL)

	// Poll for token.
	fmt.Print("Waiting for browser sign-in...")
	pollURL := fmt.Sprintf("%s/api/cli/poll?code=%s", baseURL, code)
	client := &http.Client{Timeout: 10 * time.Second}

	for i := 0; i < 120; i++ { // 10 minutes max
		time.Sleep(3 * time.Second)

		resp, err := client.Get(pollURL)
		if err != nil {
			continue
		}

		var result struct {
			Status string `json:"status"`
			Token  string `json:"token"`
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		json.Unmarshal(body, &result)

		switch result.Status {
		case "complete":
			fmt.Println()
			return result.Token, nil
		case "expired":
			fmt.Println()
			return "", fmt.Errorf("device code expired — try again")
		case "not_found":
			fmt.Println()
			return "", fmt.Errorf("device code not found — try again")
		case "pending":
			fmt.Print(".")
		}
	}

	fmt.Println()
	return "", fmt.Errorf("timed out waiting for browser authentication")
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	}
	if cmd != nil {
		cmd.Start()
	}
}

// --- Site config ---

func loadSiteConfig(siteDir string) (*SiteConfig, error) {
	tomlPath := filepath.Join(siteDir, "friendo.toml")
	cfg := &SiteConfig{}

	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
		// No config file — use directory name.
		cfg.Site.Name = filepath.Base(siteDir)
		return cfg, nil
	}

	if _, err := toml.DecodeFile(tomlPath, cfg); err != nil {
		return nil, fmt.Errorf("reading friendo.toml: %w", err)
	}

	return cfg, nil
}

func sanitizeSubdomain(name string) string {
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r == ' ' || r == '_' {
			return '-'
		}
		return -1
	}, name)
	return strings.Trim(name, "-")
}

// --- Records ---

func readLocalRecords(siteDir string) ([]map[string]any, error) {
	dbPath := filepath.Join(siteDir, "data", "friendo.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil
	}

	db, err := data.Open(siteDir)
	if err != nil {
		return nil, fmt.Errorf("opening local database: %w", err)
	}
	defer db.Close()

	names, err := db.ListCollections()
	if err != nil {
		return nil, nil
	}

	var all []map[string]any
	for _, name := range names {
		records, err := db.QueryCollection(name)
		if err != nil {
			continue
		}
		for _, r := range records {
			r["collection"] = name
			all = append(all, r)
		}
	}
	return all, nil
}

// --- File upload ---

func uploadFiles(client *Client, siteDir, siteID string) (int, error) {
	count := 0

	dirs := []string{"pages", "templates", "public"}
	for _, dir := range dirs {
		dirPath := filepath.Join(siteDir, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			continue
		}

		err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}

			rel, _ := filepath.Rel(siteDir, path)
			rel = filepath.ToSlash(rel)

			fileData, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", rel, err)
			}

			contentType := guessContentType(rel)
			if err := client.UploadAsset(siteID, rel, contentType, fileData); err != nil {
				return fmt.Errorf("uploading %s: %w", rel, err)
			}

			count++
			return nil
		})
		if err != nil {
			return count, err
		}
	}

	return count, nil
}

func guessContentType(path string) string {
	types := map[string]string{
		".html": "text/html", ".css": "text/css", ".js": "application/javascript",
		".json": "application/json", ".png": "image/png", ".jpg": "image/jpeg",
		".jpeg": "image/jpeg", ".svg": "image/svg+xml", ".gif": "image/gif",
		".woff2": "font/woff2", ".woff": "font/woff", ".ico": "image/x-icon",
	}
	ext := filepath.Ext(path)
	if ct, ok := types[ext]; ok {
		return ct
	}
	return "application/octet-stream"
}

// --- Dry run ---

func dryRun(siteDir string, cfg *SiteConfig, subdomain string) error {
	fmt.Println()
	fmt.Printf("Dry run — no changes will be made.\n\n")
	fmt.Printf("  Site:      %s\n", cfg.Site.Name)
	fmt.Printf("  Subdomain: %s.friendo.world\n", subdomain)

	if cfg.Deploy.Domain != "" {
		fmt.Printf("  Domain:    %s\n", cfg.Deploy.Domain)
	}

	records, _ := readLocalRecords(siteDir)
	fmt.Printf("  Records:   %d\n", len(records))

	count := 0
	for _, dir := range []string{"pages", "templates", "public"} {
		dirPath := filepath.Join(siteDir, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			continue
		}
		filepath.Walk(dirPath, func(_ string, info os.FileInfo, _ error) error {
			if info != nil && !info.IsDir() {
				count++
			}
			return nil
		})
	}
	fmt.Printf("  Files:     %d\n", count)

	fmt.Println()
	fmt.Println("Run without --dry-run to deploy.")
	return nil
}
