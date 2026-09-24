package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Custom domains, from the tenant's side. These drive the network's account API
// with the same cached device-auth token `friendo deploy` uses — connecting your
// own domain is something you do for your own site, not an operator action.

// DomainRecord is one DNS record the tenant has to add at their registrar.
type DomainRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
	Why   string `json:"why"`
}

type domainInstructions struct {
	Records []DomainRecord `json:"records"`
	Note    string         `json:"note"`
}

// RunDomainAdd connects a custom domain to a site the account owns and prints
// what the tenant has to do next.
func RunDomainAdd(domain, subdomain, networkURL string) error {
	token, _, sub, url, err := domainContext(subdomain, networkURL)
	if err != nil {
		return err
	}

	var out struct {
		Domain       string             `json:"domain"`
		Site         string             `json:"site"`
		Instructions domainInstructions `json:"instructions"`
		Error        string             `json:"error"`
	}
	status, err := accountPost(url+"/api/account/domains", token,
		map[string]string{"domain": domain, "subdomain": sub}, &out)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("%s", orDefault(out.Error, "could not connect that domain"))
	}

	fmt.Printf("Connected %s to %s (not live yet).\n\n", out.Domain, out.Site)
	fmt.Printf("To finish, add these records with whoever you bought %s from:\n", out.Domain)
	for i, r := range out.Instructions.Records {
		fmt.Printf("\n  %d. %s record — %s\n       Name:  %s\n       Value: %s\n",
			i+1, r.Type, r.Why, r.Name, r.Value)
	}
	if out.Instructions.Note != "" {
		fmt.Printf("\n%s\n", out.Instructions.Note)
	}
	fmt.Printf("\nDNS can take a few minutes (occasionally a few hours) to travel. Then run:\n")
	fmt.Printf("  friendo domain verify %s\n", out.Domain)
	return nil
}

// RunDomainVerify checks whether the tenant's DNS is in place and, if it is,
// puts the domain live.
func RunDomainVerify(domain, networkURL string) error {
	token, url, err := networkToken(networkURL)
	if err != nil {
		return err
	}
	var out struct {
		Domain   string `json:"domain"`
		Site     string `json:"site"`
		Verified bool   `json:"verified"`
		Error    string `json:"error"`
	}
	status, err := accountPost(url+"/api/account/domains/verify", token,
		map[string]string{"domain": domain}, &out)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("%s", orDefault(out.Error, "could not verify that domain"))
	}
	fmt.Printf("✓ %s is live — it now serves %s.\n", out.Domain, out.Site)
	return nil
}

// RunDomainList shows the account's custom domains and whether each is live.
func RunDomainList(networkURL string) error {
	token, url, err := networkToken(networkURL)
	if err != nil {
		return err
	}
	var out struct {
		Domains []struct {
			Domain string `json:"domain"`
			Site   string `json:"site"`
			Status string `json:"status"`
		} `json:"domains"`
		Error string `json:"error"`
	}
	status, err := accountGet(url+"/api/account/domains", token, &out)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("%s", orDefault(out.Error, "could not list domains"))
	}
	if len(out.Domains) == 0 {
		fmt.Println("No custom domains yet. Connect one with: friendo domain add <domain>")
		return nil
	}
	for _, d := range out.Domains {
		fmt.Printf("  %-32s → %-20s %s\n", d.Domain, d.Site, d.Status)
	}
	return nil
}

// RunDomainRemove disconnects a custom domain. The site keeps serving at its
// network address.
func RunDomainRemove(domain, networkURL string) error {
	token, url, err := networkToken(networkURL)
	if err != nil {
		return err
	}
	var out struct {
		Error string `json:"error"`
	}
	status, err := accountDelete(url+"/api/account/domains", token,
		map[string]string{"domain": domain}, &out)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("%s", orDefault(out.Error, "could not disconnect that domain"))
	}
	fmt.Printf("Disconnected %s. The site still serves at its %s address.\n", domain, strings.TrimPrefix(url, "https://"))
	return nil
}

// networkToken resolves the network URL and a valid account token, running the
// device-auth login if there isn't a cached one.
func networkToken(networkURL string) (token, url string, err error) {
	if networkURL == "" {
		networkURL = DefaultBaseURL
	}
	url = strings.TrimRight(networkURL, "/")
	token, _, err = ensureAccountToken(url)
	return token, url, err
}

// domainContext resolves the token, network URL, and which site the domain is
// for — defaulting to the site in the current folder, the way deploy does.
func domainContext(subdomain, networkURL string) (token string, cfg *SiteConfig, sub, url string, err error) {
	token, url, err = networkToken(networkURL)
	if err != nil {
		return "", nil, "", "", err
	}
	sub = strings.ToLower(strings.TrimSpace(subdomain))
	if sub == "" {
		siteDir, wderr := os.Getwd()
		if wderr != nil {
			return "", nil, "", "", wderr
		}
		cfg, err = loadSiteConfig(siteDir)
		if err != nil {
			return "", nil, "", "", fmt.Errorf(
				"run this from your site's folder, or name the site: friendo domain add <domain> --site <name>")
		}
		sub = deriveSubdomain(cfg, siteDir)
	}
	if sub == "" {
		return "", nil, "", "", fmt.Errorf("could not tell which site this is for — pass --site <name>")
	}
	return token, cfg, sub, url, nil
}

func accountGet(url, token string, out any) (int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
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

func accountDelete(url, token string, body, out any) (int, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequest(http.MethodDelete, url, bytes.NewReader(b))
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
	if out != nil && resp.StatusCode != http.StatusNoContent {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode, nil
}
