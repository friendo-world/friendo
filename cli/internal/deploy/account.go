package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DeviceLogin runs the passwordless device-auth flow against a friendo network and
// returns the linked account email + a session token to cache. Reuses openBrowser.
func DeviceLogin(baseURL string) (email, token string, err error) {
	baseURL = strings.TrimRight(baseURL, "/")
	hc := &http.Client{Timeout: 30 * time.Second}

	var start struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
		ExpiresIn       int    `json:"expires_in"`
	}
	if _, err := devicePost(hc, baseURL+"/api/auth/device/start", nil, &start); err != nil {
		return "", "", fmt.Errorf("contacting %s: %w", baseURL, err)
	}
	if start.DeviceCode == "" {
		return "", "", fmt.Errorf("%s did not start a device login (is it a friendo network?)", baseURL)
	}

	link := baseURL + start.VerificationURI
	fmt.Printf("\nTo link this device, open:\n\n    %s\n\nand enter the code:  %s\n\n", link, start.UserCode)
	openBrowser(link)

	interval := start.Interval
	if interval <= 0 {
		interval = 2
	}
	expires := start.ExpiresIn
	if expires <= 0 {
		expires = 900
	}
	deadline := time.Now().Add(time.Duration(expires) * time.Second)

	fmt.Print("Waiting for approval")
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(interval) * time.Second)
		fmt.Print(".")
		var poll struct {
			Token string `json:"token"`
			Error string `json:"error"`
		}
		status, err := devicePost(hc, baseURL+"/api/auth/device/poll", map[string]string{"device_code": start.DeviceCode}, &poll)
		if err != nil {
			continue
		}
		switch status {
		case http.StatusOK:
			if poll.Token != "" {
				fmt.Println(" ✓")
				em, _, _ := AccountInfo(baseURL, poll.Token)
				return em, poll.Token, nil
			}
		case http.StatusAccepted:
			// still pending — keep polling
		default:
			if poll.Error != "" {
				fmt.Println()
				return "", "", fmt.Errorf("%s", poll.Error)
			}
		}
	}
	fmt.Println()
	return "", "", fmt.Errorf("device authorization timed out — run it again")
}

// AccountInfo returns the account email + capabilities for a network token.
func AccountInfo(baseURL, token string) (email string, capabilities []string, err error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/account", nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return "", nil, ErrNotAuthenticated
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("account lookup failed: %s", resp.Status)
	}
	var out struct {
		Email        string   `json:"email"`
		Capabilities []string `json:"capabilities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", nil, err
	}
	return out.Email, out.Capabilities, nil
}

// RunLogin device-auths into a network (default friendo.world) and caches the session.
func RunLogin(networkURL string) error {
	if networkURL == "" {
		networkURL = DefaultBaseURL
	}
	networkURL = strings.TrimRight(networkURL, "/")

	email, token, err := DeviceLogin(networkURL)
	if err != nil {
		return err
	}
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	cfg.SetNetworkAuth(networkURL, email, token)
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("Signed in to %s as %s.\n", networkURL, email)
	return nil
}

// RunAccountWhoami prints the account you're signed in as on a network.
func RunAccountWhoami(networkURL string) error {
	if networkURL == "" {
		networkURL = DefaultBaseURL
	}
	networkURL = strings.TrimRight(networkURL, "/")

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	auth, ok := cfg.NetworkAuth(networkURL)
	if !ok || auth.Token == "" {
		return fmt.Errorf("not signed in to %s — run: friendo login %s", networkURL, networkURL)
	}
	email, caps, err := AccountInfo(networkURL, auth.Token)
	if err == ErrNotAuthenticated {
		cfg.ClearNetworkAuth(networkURL)
		cfg.Save()
		return fmt.Errorf("your session expired — run: friendo login %s", networkURL)
	}
	if err != nil {
		return err
	}
	fmt.Printf("%s on %s\n", email, networkURL)
	if len(caps) > 0 {
		fmt.Printf("  capabilities: %s\n", strings.Join(caps, ", "))
	}
	return nil
}

// RunNetworkDeploy publishes the current folder as a site the signed-in account
// owns on a friendo network (friendo.world by default). It signs in via device
// auth if needed, claims the subdomain, exchanges an in-process SSO session for
// the site, then pushes content through the normal push path.
func RunNetworkDeploy(subdomain, networkURL string) error {
	if networkURL == "" {
		networkURL = DefaultBaseURL
	}
	networkURL = strings.TrimRight(networkURL, "/")

	siteDir, err := os.Getwd()
	if err != nil {
		return err
	}
	siteCfg, err := loadSiteConfig(siteDir)
	if err != nil {
		return err
	}
	if subdomain == "" {
		subdomain = deriveSubdomain(siteCfg, siteDir)
	}
	subdomain = strings.ToLower(strings.TrimSpace(subdomain))
	if subdomain == "" {
		return fmt.Errorf("could not determine a subdomain — pass one: friendo deploy <name>")
	}

	// 1. Ensure an account session (device-auth in the browser if not cached).
	token, email, err := ensureAccountToken(networkURL)
	if err != nil {
		return err
	}

	// 2. Claim the subdomain (created + owned by this account, or verified yours).
	fmt.Printf("Claiming %s on %s …\n", subdomain, networkURL)
	var claim struct {
		Error   string `json:"error"`
		Used    int    `json:"used"`
		Allowed int    `json:"allowed"`
	}
	if status, err := accountPost(networkURL+"/api/account/sites", token,
		map[string]string{"subdomain": subdomain, "name": siteCfg.Site.Name}, &claim); err != nil {
		return err
	} else if status == http.StatusForbidden && claim.Allowed > 0 {
		// Hitting the site limit isn't a claim failure to go debug — the network's
		// message already says what happened and both ways out, so show it as-is.
		return fmt.Errorf("%s", claim.Error)
	} else if status >= 300 {
		return fmt.Errorf("could not claim %q: %s", subdomain, orDefault(claim.Error, "request failed"))
	}

	// 3. Exchange for a site admin session (in-process SSO).
	var sso struct {
		Session string `json:"session"`
		Error   string `json:"error"`
	}
	if status, err := accountPost(networkURL+"/api/sso/exchange", token,
		map[string]string{"subdomain": subdomain}, &sso); err != nil {
		return err
	} else if status >= 300 || sso.Session == "" {
		return fmt.Errorf("could not get a session for %q: %s", subdomain, orDefault(sso.Error, "no session"))
	}

	// 4. Cache the SSO session as the site's admin session and push. RunPush
	//    checks the cached site session first, so it uses this directly.
	siteURL := SiteURLForSubdomain(networkURL, subdomain)
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	cfg.SetSiteAuth(siteURL, email, sso.Session)
	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Printf("Deploying to %s\n", siteURL)
	if err := RunPush(PushOptions{Target: siteURL, Data: true}); err != nil {
		return err
	}
	fmt.Printf("\nDeployed → %s\n", siteURL)
	return nil
}

// ensureAccountToken returns a valid account token for the network, running the
// device-auth login (and caching it) when there isn't a working cached one.
func ensureAccountToken(networkURL string) (token, email string, err error) {
	cfg, err := LoadConfig()
	if err != nil {
		return "", "", err
	}
	if auth, ok := cfg.NetworkAuth(networkURL); ok && auth.Token != "" {
		if em, _, err := AccountInfo(networkURL, auth.Token); err == nil {
			return auth.Token, em, nil
		}
	}
	em, tok, err := DeviceLogin(networkURL)
	if err != nil {
		return "", "", err
	}
	cfg.SetNetworkAuth(networkURL, em, tok)
	cfg.Save()
	return tok, em, nil
}

// deriveSubdomain turns the site's name (or folder name) into a valid subdomain.
func deriveSubdomain(cfg *SiteConfig, siteDir string) string {
	name := cfg.Site.Name
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(siteDir)
	}
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_' || r == '.':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// accountPost does an authenticated JSON POST with an account Bearer token.
func accountPost(url, token string, body, out any) (int, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode, nil
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func devicePost(hc *http.Client, url string, body, out any) (int, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, url, r)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode, nil
}
