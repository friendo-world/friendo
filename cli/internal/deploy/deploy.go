package deploy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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
		events, err := readLocalEvents(siteDir)
		if err != nil {
			return err
		}
		if len(records) > 0 {
			if err := siteClient.PushData(records, events); err != nil {
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
		records, events, err := siteClient.PullDataAndEvents()
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

		// Calendar series travel with records.
		for _, row := range events {
			if ev := data.EventFromRow(row); ev != nil {
				if err := db.UpsertEvent(ev); err != nil {
					fmt.Printf("\n  Warning: failed to insert event %s: %v\n", ev.ID, err)
				}
			}
		}

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
				   avatar=excluded.avatar,
				   password_hash=CASE WHEN excluded.password_hash = '' THEN users.password_hash ELSE excluded.password_hash END,
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

// --- Browser ---

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

// SiteURLForSubdomain builds a site's URL from a network base URL and a subdomain,
// preserving the base URL's scheme and port (so it works for a local http network
// as well as a production https one). Exported for the network deploy command.
func SiteURLForSubdomain(baseURL, subdomain string) string {
	scheme := "https"
	host := baseURL
	if strings.HasPrefix(baseURL, "http://") {
		scheme, host = "http", strings.TrimPrefix(baseURL, "http://")
	} else {
		host = strings.TrimPrefix(baseURL, "https://")
	}
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	return fmt.Sprintf("%s://%s.%s", scheme, subdomain, host)
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

// readLocalEvents reads calendar series from the local database so they travel
// with their posts on push.
func readLocalEvents(siteDir string) ([]map[string]any, error) {
	dbPath := filepath.Join(siteDir, "data", "friendo.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil
	}
	db, err := data.Open(siteDir)
	if err != nil {
		return nil, fmt.Errorf("opening local database: %w", err)
	}
	defer db.Close()
	list, err := db.ListEvents()
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(list))
	for _, e := range list {
		rows = append(rows, e.Row())
	}
	return rows, nil
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
