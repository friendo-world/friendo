package deploy

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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

	"github.com/friendo-world/friendo/runtime/go/content"
	"github.com/friendo-world/friendo/runtime/go/data"
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
		Target string `toml:"target"`
		Domain string `toml:"domain"`
	} `toml:"deploy"`
}

// PushOptions controls push behavior.
type PushOptions struct {
	DryRun bool
	Data   bool
	Users  bool
	Target string
}

// PullOptions controls pull behavior.
type PullOptions struct {
	Data   bool
	Users  bool
	Target string
}

// RunDeploy is the interactive deploy wizard. apiURL overrides the platform base
// URL for this run (empty = the configured default, https://friendo.world).
func RunDeploy(apiURL string) error {
	siteDir, err := os.Getwd()
	if err != nil {
		return err
	}

	siteCfg, err := loadSiteConfig(siteDir)
	if err != nil {
		return err
	}

	// Prefer an explicit deploy target's subdomain (e.g. friendo.toml already
	// declares docs.friendo.world) over deriving one from the display name.
	subdomain := ""
	if siteCfg.Deploy.Target != "" {
		subdomain = subdomainFromTarget(siteCfg.Deploy.Target)
	}
	if subdomain == "" {
		subdomain = sanitizeSubdomain(siteCfg.Site.Name)
	}
	if subdomain == "" {
		subdomain = filepath.Base(siteDir)
	}

	fmt.Println("Where do you want to deploy?")
	fmt.Println("  1. friendo.world (managed hosting)")
	fmt.Println("  2. Cloudflare Workers (your own account)")
	fmt.Println("  3. VPS / self-hosted")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("> ")
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1":
		return deployFriendoWorld(siteDir, siteCfg, subdomain, apiURL)
	case "2":
		fmt.Println()
		fmt.Println("Cloudflare Workers deploy:")
		fmt.Println("  1. Copy runtime/edge/ to your project")
		fmt.Println("  2. Configure wrangler.toml with your D1 and R2 bindings")
		fmt.Println("  3. Run: npx wrangler deploy")
		fmt.Println("  4. Then push your site: friendo push --target https://yoursite.com")
		return nil
	case "3":
		fmt.Println()
		fmt.Println("VPS / self-hosted deploy:")
		fmt.Println("  1. Install the friendo binary on your server")
		fmt.Println("  2. Copy your site directory to the server")
		fmt.Println("  3. Run: friendo serve --port 3000")
		fmt.Println("  4. Then push updates: friendo push --target https://yoursite.com")
		return nil
	default:
		return fmt.Errorf("invalid choice %q — enter 1, 2, or 3", choice)
	}
}

func deployFriendoWorld(siteDir string, siteCfg *SiteConfig, subdomain, apiURL string) error {
	fmt.Printf("\nSite name: %s\n", subdomain)

	// Authenticate with the platform (device auth if we have no token yet).
	platform, base, err := platformClient(apiURL)
	if err != nil {
		return err
	}

	// Provision site on the platform (creates D1 + R2 + user Worker).
	target := siteURLForSubdomain(base, subdomain)
	fmt.Printf("Creating %s...", strings.TrimPrefix(target, "https://"))
	if err := platform.CreateSite(siteCfg.Site.Name, subdomain); err != nil {
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "expired") {
			clearPlatformToken()
			return fmt.Errorf("session expired — run `friendo deploy` again to re-authenticate")
		}
		return err
	}
	fmt.Println(" done")

	// Save target to friendo.toml.
	if err := saveDeployTarget(siteDir, target); err != nil {
		fmt.Printf("Warning: could not save target to friendo.toml: %v\n", err)
	}

	// Wait for the just-deployed Worker to go live (the subdomain already resolves
	// via the platform wildcard).
	fmt.Print("Waiting for site to come online..")
	if err := waitForSite(target); err != nil {
		fmt.Println(" not reachable yet")
		fmt.Printf("\n%s is provisioned. If it isn't reachable from here yet, wait a moment and finish with `friendo push --data`.\n", target)
		return fmt.Errorf("timed out waiting for %s", target)
	}
	fmt.Println(" ready")

	// Push everything to the site's sync API. The freshly provisioned site has
	// no admin yet, so RunPush -> authenticateSite walks the user through
	// creating the first admin account before uploading. Include content records
	// by default (Data) so a content/-authored site isn't deployed empty; users
	// stay opt-in via `friendo push --users`.
	fmt.Println()
	pushOpts := PushOptions{Target: target, Data: true}
	if err := RunPush(pushOpts); err != nil {
		return fmt.Errorf("push failed: %w", err)
	}

	fmt.Println()
	url := target
	if siteCfg.Deploy.Domain != "" {
		url = fmt.Sprintf("https://%s", siteCfg.Deploy.Domain)
	}
	fmt.Printf("Live at %s\n", url)
	return nil
}

// RunPush pushes local state to the deployed site via its /_/api/* endpoints.
func RunPush(opts PushOptions) error {
	siteDir, err := os.Getwd()
	if err != nil {
		return err
	}

	siteCfg, err := loadSiteConfig(siteDir)
	if err != nil {
		return err
	}

	target := resolveTarget(opts.Target, siteCfg, siteDir)

	// Compile the file-based content/ folder into the local DB first, so imported
	// records (and their assets) are included in the push.
	if content.HasContent(siteDir) {
		res, err := content.Build(siteDir)
		if err != nil {
			return fmt.Errorf("building content: %w", err)
		}
		fmt.Printf("Content: %s\n", res.Summary())
	}

	if opts.DryRun {
		subdomain := subdomainFromTarget(target)
		return dryRun(siteDir, siteCfg, subdomain)
	}

	fmt.Printf("Pushing to %s\n", target)

	// Authenticate with the site's admin (cached session, or prompt + cache).
	siteClient, err := authenticateSite(target)
	if err != nil {
		return fmt.Errorf("authenticating with %s: %w", target, err)
	}

	// Push templates.
	templates, err := readFilesAsJSON(siteDir, "pages", "layouts")
	if err != nil {
		return err
	}
	if len(templates) > 0 {
		fmt.Printf("Pushing templates...")
		if err := siteClient.PushTemplates(templates); err != nil {
			return fmt.Errorf("pushing templates: %w", err)
		}
		fmt.Printf(" %d files\n", len(templates))
	}

	// Push assets (base64-encoded so binary images round-trip intact).
	assets, err := readAssetsAsJSON(siteDir)
	if err != nil {
		return err
	}
	if len(assets) > 0 {
		fmt.Printf("Pushing assets...")
		if err := siteClient.PushAssets(assets); err != nil {
			return fmt.Errorf("pushing assets: %w", err)
		}
		fmt.Printf(" %d files\n", len(assets))
	}

	// Push records if requested.
	if opts.Data {
		fmt.Printf("Pushing records...")
		records, err := readLocalRecords(siteDir)
		if err != nil {
			return err
		}
		if len(records) > 0 {
			if err := siteClient.PushData(records); err != nil {
				return err
			}
			fmt.Printf(" %d records\n", len(records))
		} else {
			fmt.Println(" no records")
		}

		// Media rows ride along with records (the bytes went up with assets).
		files, err := readLocalFiles(siteDir)
		if err != nil {
			return err
		}
		if len(files) > 0 {
			fmt.Printf("Pushing media...")
			if err := siteClient.PushFiles(files); err != nil {
				return err
			}
			fmt.Printf(" %d files\n", len(files))
		}
	}

	// Push users (with their author profiles) if requested.
	if opts.Users {
		fmt.Printf("Pushing users...")
		users, authors, err := readLocalUsers(siteDir)
		if err != nil {
			return err
		}
		if len(users) > 0 {
			if err := siteClient.PushUsers(users, authors); err != nil {
				return err
			}
			fmt.Printf(" %d users\n", len(users))
		} else {
			fmt.Println(" no users")
		}
	}

	// Push site settings (access policy, moderation) — config travels with the
	// site so a deploy doesn't silently revert to defaults.
	if settings, err := readLocalSettings(siteDir); err == nil && len(settings) > 0 {
		fmt.Printf("Pushing settings...")
		if err := siteClient.PushSettings(settings); err != nil {
			return err
		}
		fmt.Printf(" %d\n", len(settings))
	}

	return nil
}

// RunPull fetches data from the deployed site via its /_/api/* endpoints.
func RunPull(opts PullOptions) error {
	if !opts.Data && !opts.Users {
		return fmt.Errorf("nothing to pull — use --data and/or --users")
	}

	siteDir, err := os.Getwd()
	if err != nil {
		return err
	}

	siteCfg, err := loadSiteConfig(siteDir)
	if err != nil {
		return err
	}

	target := resolveTarget(opts.Target, siteCfg, siteDir)

	siteClient, err := authenticateSite(target)
	if err != nil {
		return fmt.Errorf("authenticating with %s: %w", target, err)
	}

	db, err := data.Open(siteDir)
	if err != nil {
		return fmt.Errorf("opening local database: %w", err)
	}
	defer db.Close()

	fmt.Printf("Pulling from %s...\n", target)

	if opts.Data {
		fmt.Printf("Pulling records...")
		records, err := siteClient.PullData()
		if err != nil {
			return err
		}

		inserted := 0
		for _, r := range records {
			str := func(key string) string {
				v, _ := r[key].(string)
				return v
			}

			dataJSON := "{}"
			if d, ok := r["data"]; ok && d != nil {
				if s, isStr := d.(string); isStr {
					if s != "" {
						dataJSON = s
					}
				} else if b, err := json.Marshal(d); err == nil {
					dataJSON = string(b)
				}
			}

			_, err := db.Conn.Exec(
				`INSERT INTO posts (id, site_id, collection, slug, title, body, author_id, status, published_at, data, created, updated)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				 ON CONFLICT(id) DO UPDATE SET
				   collection=excluded.collection, slug=excluded.slug, title=excluded.title,
				   body=excluded.body, author_id=excluded.author_id, status=excluded.status,
				   published_at=excluded.published_at, data=excluded.data, updated=excluded.updated`,
				str("id"), db.SiteID, str("collection"), str("slug"), str("title"),
				str("body"), str("author_id"), str("status"),
				str("published_at"), dataJSON, str("created"), str("updated"),
			)
			if err != nil {
				fmt.Printf("\n  Warning: failed to insert record %s: %v\n", str("id"), err)
				continue
			}
			inserted++
		}
		fmt.Printf(" %d records\n", inserted)

		// Media rows travel with records.
		files, err := siteClient.PullFiles()
		if err != nil {
			return err
		}
		if len(files) > 0 {
			fmt.Printf("Pulling media...")
			pulled := 0
			for _, f := range files {
				str := func(key string) string {
					v, _ := f[key].(string)
					return v
				}
				num := func(key string) int64 {
					switch v := f[key].(type) {
					case float64:
						return int64(v)
					case int64:
						return v
					}
					return 0
				}
				if err := db.UpsertFile(str("id"), str("record_type"), str("record_id"),
					str("field"), str("r2_key"), str("mime"), num("size"), str("created")); err != nil {
					fmt.Printf("\n  Warning: failed to insert file %s: %v\n", str("id"), err)
					continue
				}
				pulled++
			}
			fmt.Printf(" %d files\n", pulled)
		}
	}

	if opts.Users {
		fmt.Printf("Pulling users...")
		users, authors, err := siteClient.PullUsers()
		if err != nil {
			return err
		}

		inserted := 0
		for _, u := range users {
			str := func(key string) string {
				v, _ := u[key].(string)
				return v
			}

			_, err := db.Conn.Exec(
				`INSERT INTO users (id, site_id, email, phone, name, avatar, password_hash, role, auth_methods, created, updated)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				 ON CONFLICT(id) DO UPDATE SET
				   email=excluded.email, phone=excluded.phone, name=excluded.name,
				   avatar=excluded.avatar, password_hash=excluded.password_hash,
				   role=excluded.role, auth_methods=excluded.auth_methods, updated=excluded.updated`,
				str("id"), db.SiteID, str("email"), str("phone"), str("name"),
				str("avatar"), str("password_hash"), str("role"),
				str("auth_methods"), str("created"), str("updated"),
			)
			if err != nil {
				fmt.Printf("\n  Warning: failed to insert user %s: %v\n", str("id"), err)
				continue
			}
			inserted++
		}

		// Author profiles travel with accounts.
		for _, a := range authors {
			str := func(key string) string { v, _ := a[key].(string); return v }
			if err := db.UpsertAuthor(str("id"), str("user_id"), str("name"), str("email"), str("avatar"), str("role"), str("created")); err != nil {
				fmt.Printf("\n  Warning: failed to insert author %s: %v\n", str("id"), err)
			}
		}
		fmt.Printf(" %d users\n", inserted)
	}

	// Pull site settings (config).
	if settings, err := siteClient.PullSettings(); err == nil {
		for k, v := range settings {
			db.SetSetting(k, v)
		}
	}

	fmt.Println("Done.")
	return nil
}

// --- Helpers ---

func resolveTarget(override string, siteCfg *SiteConfig, siteDir string) string {
	if override != "" {
		return override
	}
	if siteCfg.Deploy.Target != "" {
		return siteCfg.Deploy.Target
	}
	subdomain := sanitizeSubdomain(siteCfg.Site.Name)
	if subdomain == "" {
		subdomain = filepath.Base(siteDir)
	}
	return fmt.Sprintf("https://%s.friendo.world", subdomain)
}

func subdomainFromTarget(target string) string {
	target = strings.TrimPrefix(target, "https://")
	target = strings.TrimPrefix(target, "http://")
	parts := strings.SplitN(target, ".", 2)
	return parts[0]
}

func saveDeployTarget(siteDir, target string) error {
	tomlPath := filepath.Join(siteDir, "friendo.toml")
	content, err := os.ReadFile(tomlPath)
	if err != nil {
		return err
	}

	s := string(content)
	if strings.Contains(s, "target = ") {
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "target") && strings.Contains(trimmed, "=") {
				lines[i] = fmt.Sprintf(`target = "%s"`, target)
				break
			}
		}
		return os.WriteFile(tomlPath, []byte(strings.Join(lines, "\n")), 0o644)
	}

	if strings.Contains(s, "[deploy]") {
		s = strings.Replace(s, "[deploy]", fmt.Sprintf("[deploy]\ntarget = \"%s\"", target), 1)
		return os.WriteFile(tomlPath, []byte(s), 0o644)
	}

	s += fmt.Sprintf("\n[deploy]\ntarget = \"%s\"\n", target)
	return os.WriteFile(tomlPath, []byte(s), 0o644)
}

// readAssetsAsJSON reads the assets/ tree and base64-encodes each file's
// content so binary assets (images uploaded via the media API) survive the
// JSON round-trip — a plain string would mangle non-UTF-8 bytes. The runtime
// push/assets handlers decode entries marked `encoding: "base64"`.
func readAssetsAsJSON(siteDir string) ([]map[string]string, error) {
	dirPath := filepath.Join(siteDir, "assets")
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		return nil, nil
	}
	var files []map[string]string
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(siteDir, path)
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", rel, err)
		}
		files = append(files, map[string]string{
			"path":     filepath.ToSlash(rel),
			"content":  base64.StdEncoding.EncodeToString(content),
			"encoding": "base64",
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// readFilesAsJSON reads all files from the given directories (relative to siteDir)
// and returns them as JSON-serializable maps with path and content.
func readFilesAsJSON(siteDir string, dirs ...string) ([]map[string]string, error) {
	var files []map[string]string
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
			content, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", rel, err)
			}
			files = append(files, map[string]string{
				"path":    rel,
				"content": string(content),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

// --- Auth ---

func deviceAuth(baseURL string) (string, error) {
	codeBytes := make([]byte, 16)
	rand.Read(codeBytes)
	code := hex.EncodeToString(codeBytes)

	authURL := fmt.Sprintf("%s/cli/auth?code=%s", baseURL, code)
	fmt.Printf("If your browser doesn't open, visit:\n  %s\n\n", authURL)
	openBrowser(authURL)

	fmt.Print("Waiting for browser sign-in...")
	pollURL := fmt.Sprintf("%s/api/cli/poll?code=%s", baseURL, code)
	client := &http.Client{Timeout: 10 * time.Second}

	for i := 0; i < 120; i++ {
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

// --- Platform commands (redeploy / destroy) ---

// RedeployOptions controls redeploy behavior.
type RedeployOptions struct {
	APIURL string
}

// DestroyOptions controls destroy behavior.
type DestroyOptions struct {
	APIURL string
	Yes    bool
}

// RunRedeploy re-pushes the current runtime bundle to the site's user Worker.
func RunRedeploy(opts RedeployOptions) error {
	siteDir, err := os.Getwd()
	if err != nil {
		return err
	}
	siteCfg, err := loadSiteConfig(siteDir)
	if err != nil {
		return err
	}
	subdomain := subdomainForSite(siteCfg, siteDir)

	platform, _, err := platformClient(opts.APIURL)
	if err != nil {
		return err
	}

	fmt.Printf("Redeploying %s...", subdomain)
	if err := platform.Redeploy(subdomain); err != nil {
		fmt.Println()
		return handlePlatformErr(err, "run the command again to re-authenticate")
	}
	fmt.Println(" done")
	return nil
}

// RunDestroy deprovisions the site (tears down its Worker, D1, and R2 bucket).
func RunDestroy(opts DestroyOptions) error {
	siteDir, err := os.Getwd()
	if err != nil {
		return err
	}
	siteCfg, err := loadSiteConfig(siteDir)
	if err != nil {
		return err
	}
	subdomain := subdomainForSite(siteCfg, siteDir)

	if !opts.Yes {
		fmt.Printf("This permanently deletes %q and all its data (D1 + R2). This cannot be undone.\n", subdomain)
		reader := bufio.NewReader(os.Stdin)
		answer, _ := readLine(reader, fmt.Sprintf("Type the site name %q to confirm: ", subdomain))
		if answer != subdomain {
			return fmt.Errorf("aborted")
		}
	}

	platform, _, err := platformClient(opts.APIURL)
	if err != nil {
		return err
	}

	fmt.Printf("Destroying %s...", subdomain)
	if err := platform.Destroy(subdomain); err != nil {
		fmt.Println()
		return handlePlatformErr(err, "run the command again to re-authenticate")
	}
	fmt.Println(" done")
	return nil
}

// RunWhoami prints which friendo.world account the CLI is signed in as. Unlike
// the provisioning commands, it never triggers the device-auth browser flow —
// it only reports the state of the saved session token.
func RunWhoami(apiURL string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	base := cfg.BaseURL
	if apiURL != "" {
		base = apiURL
	}
	if cfg.Token == "" {
		fmt.Println("Not signed in. Run `friendo deploy` to sign in.")
		return nil
	}

	acct, err := NewPlatformClient(base, cfg.Token).Whoami()
	if err != nil {
		if errors.Is(err, ErrNotAuthenticated) {
			fmt.Println("Your saved session has expired. Run `friendo deploy` to sign in again.")
			return nil
		}
		return err
	}

	fmt.Printf("Signed in as %s <%s>\n", acct.Name, acct.Email)
	fmt.Printf("Platform: %s\n", base)
	return nil
}

// platformClient returns an authenticated platform API client. apiURL overrides
// the configured base URL for this run (without persisting it); if no token is
// cached yet it walks the user through device auth and saves the token.
func platformClient(apiURL string) (*PlatformClient, string, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, "", err
	}
	base := cfg.BaseURL
	if apiURL != "" {
		base = apiURL
	}

	if cfg.Token == "" {
		fmt.Println("You need to sign in. Opening your browser...")
		token, err := deviceAuth(base)
		if err != nil {
			return nil, "", fmt.Errorf("authentication failed: %w", err)
		}
		cfg.Token = token
		if err := cfg.Save(); err != nil {
			return nil, "", fmt.Errorf("saving config: %w", err)
		}
		fmt.Println("Authenticated successfully.")
	}

	return NewPlatformClient(base, cfg.Token), base, nil
}

// clearPlatformToken drops the cached platform session token.
func clearPlatformToken() {
	if cfg, err := LoadConfig(); err == nil {
		cfg.Token = ""
		cfg.Save()
	}
}

// handlePlatformErr maps expired-session errors to a clear re-auth message and
// clears the stale token; other errors pass through.
func handlePlatformErr(err error, reauthHint string) error {
	if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "expired") {
		clearPlatformToken()
		return fmt.Errorf("session expired — %s", reauthHint)
	}
	return err
}

// subdomainForSite resolves the site's subdomain from its deploy target if set,
// else from the (sanitized) site name, else the directory name.
func subdomainForSite(siteCfg *SiteConfig, siteDir string) string {
	if siteCfg.Deploy.Target != "" {
		return subdomainFromTarget(siteCfg.Deploy.Target)
	}
	s := sanitizeSubdomain(siteCfg.Site.Name)
	if s == "" {
		s = filepath.Base(siteDir)
	}
	return s
}

// siteURLForSubdomain derives a site's URL from the platform base URL's host, so
// a site on https://friendo.world lives at https://<sub>.friendo.world and one
// on https://local.friendo.world at https://<sub>.local.friendo.world.
func siteURLForSubdomain(baseURL, subdomain string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(baseURL, "https://"), "http://")
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	return fmt.Sprintf("https://%s.%s", subdomain, host)
}

// waitForSite polls a freshly provisioned site until it responds, tolerating the
// window where its DNS record isn't resolvable yet. Returns nil once the site
// answers (any HTTP status < 500), or an error after the timeout.
func waitForSite(target string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	// The subdomain resolves immediately via the platform's `*` wildcard, so this
	// only waits for the just-deployed Worker to go live (a few seconds).
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(target + "/_/api/setup")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		fmt.Print(".")
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("timed out waiting for %s to come online", target)
}

// --- Site config ---

func loadSiteConfig(siteDir string) (*SiteConfig, error) {
	tomlPath := filepath.Join(siteDir, "friendo.toml")
	cfg := &SiteConfig{}

	if _, err := os.Stat(tomlPath); os.IsNotExist(err) {
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

// readLocalFiles reads media rows from the local database so they can be pushed
// alongside records. The bytes themselves travel via the assets push.
func readLocalFiles(siteDir string) ([]map[string]any, error) {
	dbPath := filepath.Join(siteDir, "data", "friendo.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil
	}
	db, err := data.Open(siteDir)
	if err != nil {
		return nil, fmt.Errorf("opening local database: %w", err)
	}
	defer db.Close()
	return db.AllFiles()
}

func readLocalUsers(siteDir string) (users, authors []map[string]any, err error) {
	dbPath := filepath.Join(siteDir, "data", "friendo.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil, nil
	}

	db, err := data.Open(siteDir)
	if err != nil {
		return nil, nil, fmt.Errorf("opening local database: %w", err)
	}
	defer db.Close()

	list, err := db.ListUsers()
	if err != nil {
		return nil, nil, nil
	}
	for _, u := range list {
		users = append(users, map[string]any{
			"id":            u.ID,
			"email":         u.Email,
			"phone":         u.Phone,
			"name":          u.Name,
			"avatar":        u.Avatar,
			"password_hash": u.PasswordHash,
			"role":          u.Role,
			"auth_methods":  u.AuthMethods,
			"created":       u.Created,
			"updated":       u.Updated,
		})
	}
	authors, _ = db.ListAuthors()
	return users, authors, nil
}

// readLocalSettings returns the local site's settings (access policy, moderation).
func readLocalSettings(siteDir string) (map[string]string, error) {
	dbPath := filepath.Join(siteDir, "data", "friendo.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil
	}
	db, err := data.Open(siteDir)
	if err != nil {
		return nil, fmt.Errorf("opening local database: %w", err)
	}
	defer db.Close()
	return db.AllSettings()
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

	files, _ := readFilesAsJSON(siteDir, "pages", "layouts", "assets")
	fmt.Printf("  Files:     %d\n", len(files))

	fmt.Println()
	fmt.Println("Run without --dry-run to push.")
	return nil
}
