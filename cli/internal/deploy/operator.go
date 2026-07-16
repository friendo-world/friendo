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
// operator console — for managing a network's sites from your laptop.
type OperatorClient struct {
	baseURL string
	token   string
	http    *http.Client
}

// OperatorSite is a site as reported by the network operator API.
type OperatorSite struct {
	Subdomain string `json:"subdomain"`
	Name      string `json:"name"`
}

// NewOperatorClient builds a client for a network base URL with an optional
// cached operator token.
func NewOperatorClient(baseURL, token string) *OperatorClient {
	return &OperatorClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Token returns the current operator token (for caching after Login).
func (c *OperatorClient) Token() string { return c.token }

// Login exchanges operator credentials for a session token, stored on the client.
func (c *OperatorClient) Login(email, password string) error {
	var out struct {
		Token string `json:"token"`
	}
	if err := c.do(http.MethodPost, "/api/login", map[string]string{"email": email, "password": password}, &out); err != nil {
		return err
	}
	if out.Token == "" {
		return fmt.Errorf("login succeeded but no token was returned")
	}
	c.token = out.Token
	return nil
}

// Valid reports whether the current token is accepted by the network.
func (c *OperatorClient) Valid() bool {
	return c.do(http.MethodGet, "/api/whoami", nil, nil) == nil
}

// Sites lists the network's sites.
func (c *OperatorClient) Sites() ([]OperatorSite, error) {
	var out struct {
		Sites []OperatorSite `json:"sites"`
	}
	if err := c.do(http.MethodGet, "/api/sites", nil, &out); err != nil {
		return nil, err
	}
	return out.Sites, nil
}

// Provision creates a site on the network.
func (c *OperatorClient) Provision(subdomain, name string) (OperatorSite, error) {
	var out struct {
		Site OperatorSite `json:"site"`
	}
	err := c.do(http.MethodPost, "/api/sites", map[string]string{"subdomain": subdomain, "name": name}, &out)
	return out.Site, err
}

// Destroy deletes a site and all its data.
func (c *OperatorClient) Destroy(subdomain string) error {
	return c.do(http.MethodDelete, "/api/sites/"+subdomain, nil, nil)
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
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
