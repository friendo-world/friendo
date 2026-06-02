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
