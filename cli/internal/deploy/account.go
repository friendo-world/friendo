package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
