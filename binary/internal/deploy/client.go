package deploy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client talks to the friendo.world deploy API.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient creates a deploy API client.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// CreateSite registers or updates a site.
func (c *Client) CreateSite(name, subdomain string) error {
	body := map[string]string{"name": name, "subdomain": subdomain}
	resp, err := c.postJSON("/api/sites", body)
	if err != nil {
		return fmt.Errorf("registering site: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 409 {
		return fmt.Errorf("subdomain %q is already taken by another user", subdomain)
	}
	if resp.StatusCode >= 300 {
		return c.readError(resp)
	}
	return nil
}

// SyncRecords sends records to D1 via the deploy API.
func (c *Client) SyncRecords(siteID string, records []map[string]any) error {
	body := map[string]any{"records": records}
	resp, err := c.postJSON("/api/sites/"+siteID+"/sync", body)
	if err != nil {
		return fmt.Errorf("syncing records: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return c.readError(resp)
	}
	return nil
}

// UploadAsset sends a single file to R2 via the deploy API.
func (c *Client) UploadAsset(siteID, path, contentType string, data []byte) error {
	url := fmt.Sprintf("%s/api/sites/%s/assets?path=%s", c.baseURL, siteID, path)
	req, err := http.NewRequest("POST", url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("uploading %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return c.readError(resp)
	}
	return nil
}

func (c *Client) postJSON(path string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	return c.httpClient.Do(req)
}

func (c *Client) readError(resp *http.Response) error {
	data, _ := io.ReadAll(resp.Body)
	var errBody struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(data, &errBody) == nil && errBody.Error != "" {
		return fmt.Errorf("API error (%d): %s", resp.StatusCode, errBody.Error)
	}
	return fmt.Errorf("API error (%d): %s", resp.StatusCode, string(data))
}
