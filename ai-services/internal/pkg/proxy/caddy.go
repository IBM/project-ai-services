package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	// DefaultAdminURL is the default Caddy Admin API URL (localhost only for security)
	DefaultAdminURL = "http://127.0.0.1:2019"
	// DefaultServerName is the default server name in Caddy config
	DefaultServerName = "ai_services_server"
	// DefaultRequestTimeout is the default HTTP request timeout
	DefaultRequestTimeout = 30 * time.Second
)

// CaddyClient implements ProxyManager for Caddy server
type CaddyClient struct {
	config CaddyConfig
	client *http.Client
	mu     sync.Mutex // Protects concurrent API calls
}

// NewCaddyClient creates a new Caddy client with default configuration
func NewCaddyClient() *CaddyClient {
	return &CaddyClient{
		config: CaddyConfig{
			AdminURL:       DefaultAdminURL,
			ServerName:     DefaultServerName,
			HTTPSPort:      443,
			RequestTimeout: DefaultRequestTimeout,
		},
		client: &http.Client{
			Timeout: DefaultRequestTimeout,
		},
	}
}

// NewCaddyClientWithConfig creates a new Caddy client with custom configuration
func NewCaddyClientWithConfig(config CaddyConfig) *CaddyClient {
	if config.RequestTimeout == 0 {
		config.RequestTimeout = DefaultRequestTimeout
	}
	if config.AdminURL == "" {
		config.AdminURL = DefaultAdminURL
	}
	if config.ServerName == "" {
		config.ServerName = DefaultServerName
	}
	if config.HTTPSPort == 0 {
		config.HTTPSPort = 443
	}

	return &CaddyClient{
		config: config,
		client: &http.Client{
			Timeout: config.RequestTimeout,
		},
	}
}

// HealthCheck verifies Caddy is available and responding
func (c *CaddyClient) HealthCheck() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	url := fmt.Sprintf("%s/config/", c.config.AdminURL)
	resp, err := c.client.Get(url)
	if err != nil {
		return fmt.Errorf("caddy health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy health check failed with status %d: %s", resp.StatusCode, string(body))
	}

	logger.Infof("Caddy health check passed", logger.VerbosityLevelDebug)
	return nil
}

// RegisterRoute registers a new route with Caddy
func (c *CaddyClient) RegisterRoute(route Route) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	logger.Infof("Registering route: %s -> %s", route.Host, route.UpstreamAddress)

	// Construct route configuration
	routeConfig := map[string]interface{}{
		"@id": route.ID,
		"match": []map[string]interface{}{
			{
				"host": []string{route.Host},
			},
		},
		"handle": []map[string]interface{}{
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]string{
					{
						"dial": route.UpstreamAddress,
					},
				},
			},
		},
		"terminal": route.Terminal,
	}

	// Check if routes array exists, initialize if needed
	checkURL := fmt.Sprintf("%s/config/apps/http/servers/%s/routes", c.config.AdminURL, c.config.ServerName)
	checkResp, err := c.client.Get(checkURL)
	if err != nil {
		return fmt.Errorf("failed to check routes: %w", err)
	}
	defer checkResp.Body.Close()

	body, _ := io.ReadAll(checkResp.Body)
	if string(body) == "null\n" || string(body) == "null" {
		// Initialize empty routes array
		logger.Infof("Initializing routes array", logger.VerbosityLevelDebug)
		initReq, _ := http.NewRequest(http.MethodPost, checkURL, bytes.NewBufferString("[]"))
		initReq.Header.Set("Content-Type", "application/json")
		initResp, err := c.client.Do(initReq)
		if err != nil {
			return fmt.Errorf("failed to initialize routes: %w", err)
		}
		initResp.Body.Close()
	}

	// Append route to array using /-
	url := fmt.Sprintf("%s/config/apps/http/servers/%s/routes/-", c.config.AdminURL, c.config.ServerName)
	data, err := json.Marshal(routeConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal route config: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(data))
	if err != nil {
		return fmt.Errorf("failed to create post request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to register route: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy returned error (status %d): %s", resp.StatusCode, string(body))
	}

	logger.Infof("Successfully registered route: %s", route.ID)
	return nil
}

// UnregisterRoute removes a route from Caddy
func (c *CaddyClient) UnregisterRoute(routeID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	logger.Infof("Unregistering route: %s", routeID)

	// Send DELETE request to Caddy Admin API
	url := fmt.Sprintf("%s/id/%s", c.config.AdminURL, routeID)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create delete request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to unregister route: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("caddy returned error (status %d): %s", resp.StatusCode, string(body))
	}

	logger.Infof("Successfully unregistered route: %s", routeID)
	return nil
}

// LoadCertificates loads user-provided certificates into Caddy
// TODO: Implement in future commit for user-provided certificates
func (c *CaddyClient) LoadCertificates(config TLSConfig) error {
	return fmt.Errorf("user-provided certificates not yet implemented")
}

// GetRoutes retrieves all registered routes from Caddy
func (c *CaddyClient) GetRoutes() ([]Route, error) {
	// TODO: Implement when needed for route listing
	return nil, fmt.Errorf("not implemented yet")
}

// Made with Bob
