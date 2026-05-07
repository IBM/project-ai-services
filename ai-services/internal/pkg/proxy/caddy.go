package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// defaultAdminURL is the default Caddy Admin API URL (localhost only for security)
	defaultAdminURL = "http://127.0.0.1:2019"
	// defaultRequestTimeout is the default HTTP request timeout
	defaultRequestTimeout = 30 * time.Second
)

// CaddyClient manages interactions with Caddy Admin API
type CaddyClient struct {
	adminURL string
	client   *http.Client
}

// NewCaddyClient creates a new Caddy client with default configuration
func NewCaddyClient() *CaddyClient {
	return &CaddyClient{
		adminURL: defaultAdminURL,
		client: &http.Client{
			Timeout: defaultRequestTimeout,
		},
	}
}

// HealthCheck checks if Caddy Admin API is accessible
func (c *CaddyClient) HealthCheck() error {
	url := fmt.Sprintf("%s/config/", c.adminURL)

	resp, err := c.client.Get(url)
	if err != nil {
		return fmt.Errorf("caddy admin API is not accessible: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("caddy admin API returned status %d", resp.StatusCode)
	}

	return nil
}

// GetConfig retrieves the current Caddy configuration
func (c *CaddyClient) GetConfig() (map[string]interface{}, error) {
	url := fmt.Sprintf("%s/config/", c.adminURL)

	resp, err := c.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("caddy returned error (status %d): %s", resp.StatusCode, string(body))
	}

	var config map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode config: %w", err)
	}

	return config, nil
}

// Made with Bob
