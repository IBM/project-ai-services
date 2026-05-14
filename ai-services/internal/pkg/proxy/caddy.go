package proxy

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// caddyManager implements ProxyManager interface for Caddy
type caddyManager struct {
	client     *resty.Client
	adminURL   string
	serverName string
}

// NewCaddyManager creates a new Caddy proxy manager
func NewCaddyManager(adminURL, serverName string) ProxyManager {
	client := resty.New().
		SetTimeout(10 * time.Second).
		SetRetryCount(3).
		SetRetryWaitTime(1 * time.Second).
		SetRetryMaxWaitTime(5 * time.Second)

	return &caddyManager{
		client:     client,
		adminURL:   adminURL,
		serverName: serverName,
	}
}

// HealthCheck verifies Caddy is running and accessible
func (c *caddyManager) HealthCheck() error {
	url := fmt.Sprintf("%s/config/", c.adminURL)
	resp, err := c.client.R().Get(url)
	
	if err != nil {
		return fmt.Errorf("failed to connect to Caddy admin API: %w", err)
	}
	
	if resp.StatusCode() != 200 {
		return fmt.Errorf("caddy admin API returned status %d", resp.StatusCode())
	}
	
	return nil
}

// RegisterRoute registers a new route with Caddy by appending to the routes array
func (c *caddyManager) RegisterRoute(route Route) error {
	// Build Caddy route configuration
	routeConfig := map[string]interface{}{
		"@id": route.ID,
		"match": []map[string]interface{}{
			{
				"host": []string{route.Domain},
			},
		},
		"handle": []map[string]interface{}{
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]interface{}{
					{
						"dial": route.Upstream,
					},
				},
			},
		},
		"terminal": route.Terminal,
	}

	// Check if routes array exists, initialize if needed
	checkURL := fmt.Sprintf("%s/config/apps/http/servers/%s/routes", c.adminURL, c.serverName)
	checkResp, err := c.client.R().Get(checkURL)
	if err != nil {
		return fmt.Errorf("failed to check routes: %w", err)
	}

	// Initialize empty routes array if it doesn't exist (response is "null")
	respBody := strings.TrimSpace(checkResp.String())
	if respBody == "null" {
		_, err := c.client.R().
			SetHeader("Content-Type", "application/json").
			SetBody("[]").
			Post(checkURL)
		if err != nil {
			return fmt.Errorf("failed to initialize routes: %w", err)
		}
	}

	// Append route to array using /-
	appendURL := fmt.Sprintf("%s/config/apps/http/servers/%s/routes/-", c.adminURL, c.serverName)
	resp, err := c.client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(routeConfig).
		Post(appendURL)

	if err != nil {
		return fmt.Errorf("failed to register route: %w", err)
	}

	if resp.StatusCode() != 200 {
		return fmt.Errorf("caddy returned status %d: %s", resp.StatusCode(), resp.String())
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
		logger.Warningf("Failed to get Caddy admin port: %v", err)
		logger.Infoln("Routes not registered. You can manually configure them later")
		return fmt.Errorf("failed to get Caddy admin port: %w", err)
	}

	// Step 2: Create proxy manager with the discovered admin URL
	adminURL := fmt.Sprintf("http://localhost:%s", adminPort)
	proxyManager := NewCaddyManager(adminURL, serverName)

	// Step 3: Perform health check on Caddy
	if err := proxyManager.HealthCheck(); err != nil {
		logger.Warningf("Caddy not ready: %v", err)
		logger.Infoln("Routes not registered. You can manually configure them later")
		return fmt.Errorf("caddy health check failed: %w", err)
	}

	// Step 4: Get host IP for route domain generation
	hostIP, err := utils.GetHostIP()
	if err != nil {
		logger.Warningf("Failed to get host IP: %v", err)
		return fmt.Errorf("failed to get host IP: %w", err)
	}

	// Step 5: Build routes from routes_file.yaml configuration
	routes, err := BuildRoutesFromConfig(tp, appTemplate, hostIP)
	if err != nil {
		logger.Warningf("Failed to build routes from config: %v", err)
		return fmt.Errorf("failed to build routes: %w", err)
	}

	// Step 6: Register each route with Caddy
	var registrationErrors []error
	for _, route := range routes {
		if err := proxyManager.RegisterRoute(route); err != nil {
			logger.Warningf("Failed to register route %s: %v", route.ID, err)
			registrationErrors = append(registrationErrors, fmt.Errorf("route %s: %w", route.ID, err))
		}
	}

	// Return error if any routes failed to register
	if len(registrationErrors) > 0 {
		return fmt.Errorf("failed to register %d route(s)", len(registrationErrors))
	}

	return nil
}

// Made with Bob