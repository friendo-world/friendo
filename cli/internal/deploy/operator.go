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

// OperatorClient talks to a friendo network's operator API (the apex `/api/*`
// endpoints) as an authenticated operator. It's the CLI counterpart to the
// network console — for managing a network from your laptop. It carries an
// account session token (from `friendo login`); the network gates the operator
// API on the account's operator capability.
type OperatorClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// OperatorSite is a site as reported by the network operator API.
type OperatorSite struct {
	Subdomain string   `json:"subdomain"`
	Name      string   `json:"name"`
	Owner     string   `json:"owner"`
	Suspended bool     `json:"suspended"`
	Reason    string   `json:"reason"`
	Domains   []Domain `json:"domains"`
}

// Domain is a custom domain as the network reports it.
type Domain struct {
	Domain   string `json:"domain"`
	Site     string `json:"site"`
	Verified bool   `json:"verified"`
	Status   string `json:"status"`
}

// OperatorAccount is an account row from the network's People list.
type OperatorAccount struct {
	ID        string   `json:"id"`
	Email     string   `json:"email"`
	Operator  bool     `json:"operator"`
	Used      int      `json:"used"`
	Allowed   int      `json:"allowed"`
	Unlimited bool     `json:"unlimited"`
	Override  bool     `json:"override"`
	Sites     []string `json:"sites"`
	Suspended bool     `json:"suspended"`
	Reason    string   `json:"reason"`
}

// AllowedLabel renders the cap ("unlimited" rather than "0").
func (a OperatorAccount) AllowedLabel() string {
	if a.Unlimited {
		return "unlimited"
	}
	return fmt.Sprintf("%d", a.Allowed)
}

// Invite is an outstanding invitation.
type Invite struct {
	Email     string `json:"email"`
	Status    string `json:"status"`
	Expires   string `json:"expires"`
	InvitedBy string `json:"invited_by"`
}

// NetworkSummary is the network's settings + counts.
type NetworkSummary struct {
	Base         string         `json:"base"`
	Signups      string         `json:"signups"`
	DefaultQuota string         `json:"default_quota"`
	HomeSite     string         `json:"home_site"`
	Counts       map[string]int `json:"counts"`
}

// NewOperatorClient builds a client for a network base URL with a cached account
// token (obtained via `friendo login`).
func NewOperatorClient(baseURL, token string) *OperatorClient {
	return &OperatorClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Valid reports whether the current token is accepted by the network.
func (c *OperatorClient) Valid() bool {
	return c.do(http.MethodGet, "/api/whoami", nil, nil) == nil
}

// Summary fetches the network's settings and counts.
func (c *OperatorClient) Summary() (NetworkSummary, error) {
	var out NetworkSummary
	err := c.do(http.MethodGet, "/api/network", nil, &out)
	return out, err
}

// UpdateSettings changes any of the network knobs; nil fields are left alone.
func (c *OperatorClient) UpdateSettings(signups, defaultQuota, homeSite *string) (NetworkSummary, error) {
	body := map[string]any{}
	if signups != nil {
		body["signups"] = *signups
	}
	if defaultQuota != nil {
		body["default_quota"] = *defaultQuota
	}
	if homeSite != nil {
		body["home_site"] = *homeSite
	}
	var out NetworkSummary
	err := c.do(http.MethodPut, "/api/network/settings", body, &out)
	return out, err
}

// Sites lists the network's sites.
func (c *OperatorClient) Sites() ([]OperatorSite, error) {
	var out struct {
		Sites []OperatorSite `json:"sites"`
	}
	if err := c.do(http.MethodGet, "/api/network/sites", nil, &out); err != nil {
		return nil, err
	}
	return out.Sites, nil
}

// Provision creates a site on the network, owned by owner (or by you when "").
func (c *OperatorClient) Provision(subdomain, name, owner string) (OperatorSite, error) {
	var out struct {
		Site OperatorSite `json:"site"`
	}
	err := c.do(http.MethodPost, "/api/sites", map[string]string{"subdomain": subdomain, "name": name, "owner": owner}, &out)
	return out.Site, err
}

// Destroy deletes a site and all its data.
func (c *OperatorClient) Destroy(subdomain string) error {
	return c.do(http.MethodDelete, "/api/sites/"+subdomain, nil, nil)
}

// SuspendSite puts a site on hold; ResumeSite puts it back.
func (c *OperatorClient) SuspendSite(subdomain, reason string) error {
	return c.do(http.MethodPost, "/api/network/sites/"+subdomain+"/suspend", map[string]string{"reason": reason}, nil)
}
func (c *OperatorClient) ResumeSite(subdomain string) error {
	return c.do(http.MethodPost, "/api/network/sites/"+subdomain+"/resume", nil, nil)
}

// SetOwner hands a site to another account.
func (c *OperatorClient) SetOwner(subdomain, email string) error {
	return c.do(http.MethodPut, "/api/network/sites/"+subdomain+"/owner", map[string]string{"email": email}, nil)
}

// Accounts lists everyone on the network with their usage and status.
func (c *OperatorClient) Accounts() ([]OperatorAccount, error) {
	var out struct {
		Accounts []OperatorAccount `json:"accounts"`
	}
	if err := c.do(http.MethodGet, "/api/network/accounts", nil, &out); err != nil {
		return nil, err
	}
	return out.Accounts, nil
}

// Account finds one account by email, with guidance when it doesn't exist.
func (c *OperatorClient) Account(email string) (OperatorAccount, error) {
	list, err := c.Accounts()
	if err != nil {
		return OperatorAccount{}, err
	}
	email = strings.ToLower(strings.TrimSpace(email))
	for _, a := range list {
		if a.Email == email {
			return a, nil
		}
	}
	return OperatorAccount{}, fmt.Errorf("no account for %q — see who exists with: friendo network accounts", email)
}

// SuspendAccount blocks an account from signing in or creating sites. Sessions
// it already holds stop working at once (the network checks on every request).
func (c *OperatorClient) SuspendAccount(id, reason string) error {
	return c.do(http.MethodPost, "/api/network/accounts/"+id+"/suspend", map[string]string{"reason": reason}, nil)
}
func (c *OperatorClient) ResumeAccount(id string) error {
	return c.do(http.MethodPost, "/api/network/accounts/"+id+"/resume", nil, nil)
}

// SignOutAccount ends every session an account holds; returns how many.
func (c *OperatorClient) SignOutAccount(id string) (int, error) {
	var out struct {
		Ended int `json:"ended"`
	}
	err := c.do(http.MethodPost, "/api/network/accounts/"+id+"/signout", nil, &out)
	return out.Ended, err
}

// SetAccountQuota sets one account's own limit ("5", "unlimited", or "default").
func (c *OperatorClient) SetAccountQuota(id, sites string) error {
	return c.do(http.MethodPut, "/api/network/accounts/"+id+"/quota", map[string]string{"sites": sites}, nil)
}

// Invites lists outstanding invitations.
func (c *OperatorClient) Invites() ([]Invite, error) {
	var out struct {
		Invites []Invite `json:"invites"`
	}
	if err := c.do(http.MethodGet, "/api/network/invites", nil, &out); err != nil {
		return nil, err
	}
	return out.Invites, nil
}

// Invite mints an invitation good for days (0 = the network default).
func (c *OperatorClient) Invite(email string, days int) (Invite, error) {
	var out Invite
	err := c.do(http.MethodPost, "/api/network/invites", map[string]any{"email": email, "days": days}, &out)
	return out, err
}
func (c *OperatorClient) RevokeInvite(email string) error {
	return c.do(http.MethodDelete, "/api/network/invites/"+email, nil, nil)
}

// PruneInvites forgets expired invites; returns how many.
func (c *OperatorClient) PruneInvites() (int, error) {
	var out struct {
		Removed int `json:"removed"`
	}
	err := c.do(http.MethodPost, "/api/network/invites/prune", nil, &out)
	return out.Removed, err
}

// GrantOperator / RevokeOperator manage who runs the network.
func (c *OperatorClient) GrantOperator(email string) error {
	return c.do(http.MethodPost, "/api/network/operators", map[string]string{"email": email}, nil)
}
func (c *OperatorClient) RevokeOperator(email string) error {
	return c.do(http.MethodDelete, "/api/network/operators/"+email, nil, nil)
}

// do performs a JSON request, decoding a 2xx body into out (if non-nil) and
// turning a non-2xx response into an error carrying the server's message.
func (c *OperatorClient) do(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("contacting %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrNotAuthenticated
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return fmt.Errorf("%s", e.Error)
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
