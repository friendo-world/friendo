package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// PlatformClient talks to the friendo.world platform API for provisioning.
type PlatformClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewPlatformClient creates a platform API client.
func NewPlatformClient(baseURL, token string) *PlatformClient {
	return &PlatformClient{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// CreateSite registers or updates a site on the platform.
func (c *PlatformClient) CreateSite(name, subdomain string) error {
	body := map[string]string{"name": name, "subdomain": subdomain}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", c.baseURL+"/api/sites", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("registering site: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("registering site: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 409 {
		return fmt.Errorf("subdomain %q is already taken by another user", subdomain)
	}
	if resp.StatusCode >= 300 {
		return readError(resp)
	}
	return nil
}

// Redeploy re-pushes the current runtime bundle to an existing site's user
// Worker (POST /api/sites/:id/redeploy). Data is untouched.
func (c *PlatformClient) Redeploy(subdomain string) error {
	req, err := http.NewRequest("POST", c.baseURL+"/api/sites/"+subdomain+"/redeploy", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("redeploy request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return readError(resp)
	}
	return nil
}

// Destroy deprovisions a site: tears down its Worker, D1, and R2 bucket and
// removes it from the platform registry (DELETE /api/sites/:id).
func (c *PlatformClient) Destroy(subdomain string) error {
	req, err := http.NewRequest("DELETE", c.baseURL+"/api/sites/"+subdomain, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("destroy request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return readError(resp)
	}
	return nil
}

// SiteClient talks to a site's /_/api/* sync endpoints.
// Auth is via the site admin session cookie.
type SiteClient struct {
	siteURL    string // e.g. "https://testsite.friendo.world" or "http://localhost:8788"
	cookie     string // friendo_session cookie value
	httpClient *http.Client
}

// NewSiteClient creates a sync API client for a specific site.
func NewSiteClient(siteURL, sessionCookie string) *SiteClient {
	return &SiteClient{
		siteURL: siteURL,
		cookie:  sessionCookie,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Cookie returns the current session cookie value (for caching).
func (c *SiteClient) Cookie() string { return c.cookie }

// Login authenticates with the site admin's email + password and stores the
// resulting friendo_session cookie on the client.
func (c *SiteClient) Login(email, password string) error {
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", c.siteURL+"/_/api/auth/login", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid email or password")
	}
	if resp.StatusCode >= 300 {
		return readError(resp)
	}

	for _, ck := range resp.Cookies() {
		if ck.Name == "friendo_session" && ck.Value != "" {
			c.cookie = ck.Value
			return nil
		}
	}
	return fmt.Errorf("login succeeded but no session cookie was returned")
}

// NeedsSetup reports whether the site has no admin account yet (first run).
func (c *SiteClient) NeedsSetup() (bool, error) {
	var result struct {
		NeedsSetup bool `json:"needsSetup"`
	}
	if err := c.get("/_/api/setup", &result); err != nil {
		return false, err
	}
	return result.NeedsSetup, nil
}

// Setup creates the site's first admin (superadmin) and stores the resulting
// session cookie on the client. Only works while the site has no users.
func (c *SiteClient) Setup(email, name, password string) error {
	body, err := json.Marshal(map[string]string{"email": email, "name": name, "password": password})
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", c.siteURL+"/_/api/setup", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("setup request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("this site already has an admin account")
	}
	if resp.StatusCode >= 300 {
		return readError(resp)
	}

	for _, ck := range resp.Cookies() {
		if ck.Name == "friendo_session" && ck.Value != "" {
			c.cookie = ck.Value
			return nil
		}
	}
	return fmt.Errorf("setup succeeded but no session cookie was returned")
}

// SessionValid reports whether the current cookie authenticates (GET /_/api/me).
func (c *SiteClient) SessionValid() bool {
	if c.cookie == "" {
		return false
	}
	req, err := http.NewRequest("GET", c.siteURL+"/_/api/me", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Cookie", "friendo_session="+c.cookie)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// PushTemplates uploads template files to the site.
func (c *SiteClient) PushTemplates(files []map[string]string) error {
	body := map[string]any{"files": files}
	return c.post("/_/api/push/templates", body)
}

// PushAssets uploads static asset files to the site.
func (c *SiteClient) PushAssets(files []map[string]string) error {
	body := map[string]any{"files": files}
	return c.post("/_/api/push/assets", body)
}

// PushData upserts records into the site's database.
func (c *SiteClient) PushData(records []map[string]any) error {
	body := map[string]any{"records": records}
	return c.post("/_/api/push/data", body)
}

// PushUsers upserts user accounts into the site's database.
func (c *SiteClient) PushUsers(users []map[string]any) error {
	body := map[string]any{"users": users}
	return c.post("/_/api/push/users", body)
}

// PullData fetches all records from the site.
func (c *SiteClient) PullData() ([]map[string]any, error) {
	var result struct {
		Records []map[string]any `json:"records"`
	}
	if err := c.get("/_/api/pull/data", &result); err != nil {
		return nil, err
	}
	return result.Records, nil
}

// PullUsers fetches all user accounts from the site.
func (c *SiteClient) PullUsers() ([]map[string]any, error) {
	var result struct {
		Users []map[string]any `json:"users"`
	}
	if err := c.get("/_/api/pull/users", &result); err != nil {
		return nil, err
	}
	return result.Users, nil
}

func (c *SiteClient) post(path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("POST", c.siteURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cookie != "" {
		req.Header.Set("Cookie", "friendo_session="+c.cookie)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request to %s failed: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return readError(resp)
	}
	return nil
}

func (c *SiteClient) get(path string, result any) error {
	req, err := http.NewRequest("GET", c.siteURL+path, nil)
	if err != nil {
		return err
	}
	if c.cookie != "" {
		req.Header.Set("Cookie", "friendo_session="+c.cookie)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request to %s failed: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return readError(resp)
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

func readError(resp *http.Response) error {
	data, _ := io.ReadAll(resp.Body)
	var errBody struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &errBody) == nil && errBody.Error != "" {
		return fmt.Errorf("API error (%d): %s", resp.StatusCode, errBody.Error)
	}
	return fmt.Errorf("API error (%d): %s", resp.StatusCode, string(data))
}
