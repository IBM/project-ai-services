package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// caddyManager implements ProxyManager interface for Caddy.
type caddyManager struct {
	httpClient *resty.Client
	adminURL   string
	serverName string
}

const (
	Timeout          = 10 * time.Second
	RetryCount       = 3
	RetryWaitTime    = 1 * time.Second
	RetryMaxWaitTime = 5 * time.Second
)

// NewCaddyManager creates a new Caddy proxy manager.
func NewCaddyManager(adminURL, serverName string) ProxyManager {
	httpClient := resty.New().
		SetTimeout(Timeout).
		SetRetryCount(RetryCount).
		SetRetryWaitTime(RetryWaitTime).
		SetRetryMaxWaitTime(RetryMaxWaitTime)

	return &caddyManager{
		httpClient: httpClient,
		adminURL:   adminURL,
		serverName: serverName,
	}
}

// HealthCheck verifies Caddy is running and accessible.
func (c *caddyManager) HealthCheck() error {
	url, err := url.JoinPath(c.adminURL, "config")
	if err != nil {
		return err
	}
	resp, err := c.httpClient.R().Get(url)

	if err != nil {
		return fmt.Errorf("failed to connect to Caddy admin API: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("caddy admin API returned status %d", resp.StatusCode())
	}

	return nil
}

func (c *caddyManager) RegisterRoute(route Route) error {
	if route.ID == "" {
		return fmt.Errorf("cannot register route: route ID is empty")
	}

	routeConfig := map[string]interface{}{
		"@id":   route.ID,
		"match": []map[string]interface{}{{"host": []string{route.Domain}}},
		"handle": []map[string]interface{}{{
			"handler":   "reverse_proxy",
			"upstreams": []map[string]interface{}{{"dial": route.Upstream}},
		}},
		"terminal": route.Terminal,
	}

	idURL, err := url.JoinPath(c.adminURL, "id", route.ID)
	if err != nil {
		return err
	}

	checkResp, err := c.httpClient.R().Get(idURL)
	if err != nil {
		return fmt.Errorf("failed to check route: %w", err)
	}

	switch checkResp.StatusCode() {
	case http.StatusOK:
		return c.updateRoute(idURL, routeConfig)
	case http.StatusNotFound:
		return c.createRoute(routeConfig)
	default:
		return fmt.Errorf("unexpected status checking route: %d", checkResp.StatusCode())
	}
}

// Helper to update an existing route via its specific ID URL.
func (c *caddyManager) updateRoute(idURL string, routeConfig map[string]interface{}) error {
	resp, err := c.httpClient.R().
		SetHeader("Content-Type", "application/json").
		SetBody(routeConfig).
		Put(idURL)
	if err != nil {
		return fmt.Errorf("failed to update route: %w", err)
	}
	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusCreated {
		return fmt.Errorf("caddy returned status %d on update: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// Helper to append a new route to the server's route array.
func (c *caddyManager) createRoute(routeConfig map[string]interface{}) error {
	routeURL, err := url.JoinPath(c.adminURL, "config", "apps", "http", "servers", c.serverName, "routes")
	if err != nil {
		return err
	}

	resp, err := c.httpClient.R().
		SetHeader("Content-Type", "application/json").
		SetBody(routeConfig).
		Post(routeURL)
	if err != nil {
		return fmt.Errorf("failed to create route: %w", err)
	}
	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusCreated {
		return fmt.Errorf("caddy returned status %d on creation: %s", resp.StatusCode(), resp.String())
	}

	return nil
}

// RegisterRoutesForApp registers routes for an application with Caddy proxy.
// This is a reusable function that can be called from catalog, application, or service deployments.
//
// Parameters:
//   - rt: Runtime interface for interacting with pods
//   - tp: Template provider for loading routes configuration
//   - appName: Name of the application (e.g., "ai-services" for catalog)
//   - appTemplate: Template name (e.g., "catalog", "rag", "chat")
//   - serverName: Caddy server name (e.g., "my_app_server")
//
// Returns:
//   - error: nil if routes were registered successfully, error otherwise
//
// The function performs the following steps:
//  1. Discovers Caddy admin port from pod port mappings
//  2. Creates a proxy manager with the admin URL
//  3. Performs health check on Caddy
//  4. Builds routes from routes_file.yaml configuration
//  5. Registers each route with Caddy
//
// If any step fails, appropriate warnings are logged and the function returns early.
func RegisterRoutesForApp(
	rt runtime.Runtime,
	tp templates.Template,
	appName string,
	appTemplate string,
	serverName string,
) error {
	// Step 1: Get Caddy admin port from pod port mappings
	adminPort, err := GetCaddyAdminPort(rt, appName)
	if err != nil {
		return fmt.Errorf(
			"failed to get Caddy admin port, routes not registered: %w",
			err,
		)
	}

	// Step 2: Create proxy manager with the discovered admin URL
	adminURL := fmt.Sprintf("http://localhost:%s", adminPort)
	proxyManager := NewCaddyManager(adminURL, serverName)

	// Step 3: Perform health check on Caddy
	if err := proxyManager.HealthCheck(); err != nil {
		return fmt.Errorf(
			"caddy health check failed, routes not registered: %w",
			err,
		)
	}

	// Step 4: Get host IP for route domain generation
	hostIP, err := utils.GetHostIP()
	if err != nil {
		return fmt.Errorf("failed to get host IP: %w", err)
	}

	// Step 5: Build routes from routes_file.yaml configuration
	routes, err := BuildRoutesFromConfig(tp, appTemplate, hostIP)
	if err != nil {
		return fmt.Errorf("failed to build routes: %w", err)
	}

	// Step 6: Register each route with Caddy
	var registrationErrors []error
	for _, route := range routes {
		if err := proxyManager.RegisterRoute(route); err != nil {
			registrationErrors = append(registrationErrors, fmt.Errorf("route %s: %w", route.ID, err))
		}
	}

	// Return error if any routes failed to register
	if len(registrationErrors) > 0 {
		return fmt.Errorf("failed to register %d route(s): %w", len(registrationErrors), errors.Join(registrationErrors...))
	}

	return nil
}

// Made with Bob
