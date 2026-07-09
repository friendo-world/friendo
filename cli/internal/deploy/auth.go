package deploy

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"

	"golang.org/x/term"
)

// authenticateSite returns a SiteClient with a valid admin session for target.
//
// It reuses a cached session when possible. For friendo.world sites the deploying
// account is the superadmin, so it signs in via platform SSO (no site password).
// For self-hosted / custom-domain sites it walks the user through creating the
// first admin via /_/api/setup, or prompts for email + password if one exists.
// The resulting session is cached in ~/.friendo/config.
func authenticateSite(target string) (*SiteClient, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return nil, err
	}

	// Reuse a cached session if it's still valid.
	if auth, ok := cfg.SiteAuth(target); ok && auth.Cookie != "" {
		client := NewSiteClient(target, auth.Cookie)
		if client.SessionValid() {
			return client, nil
		}
		fmt.Println("Your saved session has expired — signing in again.")
		cfg.ClearSiteAuth(target)
	}

	// friendo.world-hosted sites: hand the platform owner a superadmin session
	// via SSO, so deploy needs no separate site password.
	if isPlatformTarget(target, cfg.BaseURL) {
		return authenticateSiteViaSSO(target)
	}

	client := NewSiteClient(target, "")

	needsSetup, err := client.NeedsSetup()
	if err != nil {
		return nil, fmt.Errorf("contacting %s: %w", target, err)
	}

	var email string
	if needsSetup {
		email, err = setupFirstAdmin(target, client)
	} else {
		email, err = loginExistingAdmin(target, client)
	}
	if err != nil {
		return nil, err
	}

	cfg.SetSiteAuth(target, email, client.Cookie())
	if err := cfg.Save(); err != nil {
		fmt.Printf("Warning: could not cache session: %v\n", err)
	}
	return client, nil
}

// isPlatformTarget reports whether target is a site hosted on the friendo.world
// platform, i.e. a subdomain of the platform base host (friendo.world or the dev
// local.friendo.world). Custom-domain and VPS targets are not.
func isPlatformTarget(target, baseURL string) bool {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	b, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	t, err := url.Parse(target)
	if err != nil {
		return false
	}
	return strings.HasSuffix(t.Hostname(), "."+b.Hostname())
}

// authenticateSiteViaSSO signs into a friendo.world site as the platform owner:
// it obtains a platform session (device-auth if needed), mints a one-time SSO
// code, and redeems it for a superadmin friendo_session on the site.
func authenticateSiteViaSSO(target string) (*SiteClient, error) {
	pc, _, err := platformClient("")
	if err != nil {
		return nil, err
	}

	subdomain := subdomainFromTarget(target)
	code, err := pc.SSOCode(subdomain)
	if err != nil {
		return nil, handlePlatformErr(err, "run `friendo deploy` to sign in again")
	}

	client := NewSiteClient(target, "")
	if err := client.PlatformLogin(code); err != nil {
		return nil, err
	}

	// Cache the session, tagged with the platform account for display.
	email := ""
	if acct, err := pc.Whoami(); err == nil {
		email = acct.Email
	}
	if cfg, err := LoadConfig(); err == nil {
		cfg.SetSiteAuth(target, email, client.Cookie())
		if err := cfg.Save(); err != nil {
			fmt.Printf("Warning: could not cache session: %v\n", err)
		}
	}
	return client, nil
}

// setupFirstAdmin creates the first admin account on an empty site.
func setupFirstAdmin(target string, client *SiteClient) (string, error) {
	fmt.Printf("%s has no admin account yet — let's create one.\n", target)

	fd := int(os.Stdin.Fd())
	reader := bufio.NewReader(os.Stdin)

	email, err := readLine(reader, "Email: ")
	if err != nil {
		return "", err
	}
	name, err := readLine(reader, "Name (optional): ")
	if err != nil {
		return "", err
	}
	password, err := readPassword(reader, fd, "Password (min 8 chars): ")
	if err != nil {
		return "", err
	}

	if err := client.Setup(email, name, password); err != nil {
		return "", err
	}
	fmt.Println("Admin account created.")
	return email, nil
}

// loginExistingAdmin authenticates against a site that already has an admin.
func loginExistingAdmin(target string, client *SiteClient) (string, error) {
	fmt.Printf("Log in to %s\n", target)

	fd := int(os.Stdin.Fd())
	reader := bufio.NewReader(os.Stdin)

	email, err := readLine(reader, "Email: ")
	if err != nil {
		return "", err
	}
	password, err := readPassword(reader, fd, "Password: ")
	if err != nil {
		return "", err
	}

	if err := client.Login(email, password); err != nil {
		return "", err
	}
	return email, nil
}

// readLine reads and trims a single echoed line from the reader.
func readLine(reader *bufio.Reader, prompt string) (string, error) {
	fmt.Print(prompt)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// readPassword reads a password without echo on an interactive terminal,
// falling back to the buffered reader for piped (non-interactive) stdin.
func readPassword(reader *bufio.Reader, fd int, prompt string) (string, error) {
	fmt.Print(prompt)
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil {
			return "", fmt.Errorf("reading password: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	return strings.TrimSpace(line), nil
}
