package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
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

// extractDomainFromRoute extracts the domain from a Caddy route configuration.
// Returns the domain and a boolean indicating success.
func extractDomainFromRoute(rawRoute map[string]interface{}) (string, bool) {
	matches, ok := rawRoute["match"].([]interface{})
	if !ok || len(matches) == 0 {
		return "", false
	}

	firstMatch, ok := matches[0].(map[string]interface{})
	if !ok {
		return "", false
	}

	hosts, ok := firstMatch["host"].([]interface{})
	if !ok || len(hosts) == 0 {
		return "", false
	}

	domain, ok := hosts[0].(string)
	if !ok || domain == "" {
		return "", false
	}

	return domain, true
}

// GetRegisteredRoutes retrieves all routes currently registered with Caddy.
// Only extracts route ID and domain for efficiency.
func (c *caddyManager) GetRegisteredRoutes() ([]Route, error) {
	// Build URL to get routes from Caddy config
	routesURL, err := url.JoinPath(c.adminURL, "config", "apps", "http", "servers", c.serverName, "routes")
	if err != nil {
		return nil, fmt.Errorf("failed to build routes URL: %w", err)
	}

	// Query Caddy for registered routes
	var rawRoutes []map[string]interface{}
	resp, err := c.httpClient.R().
		SetResult(&rawRoutes).
		Get(routesURL)
	if err != nil {
		return nil, fmt.Errorf("failed to query Caddy routes: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("caddy returned status %d when querying routes", resp.StatusCode())
	}

	// Parse routes from Caddy response - only extract ID and domain
	routes := []Route{}
	for _, rawRoute := range rawRoutes {
		// Extract route ID
		routeID, ok := rawRoute["@id"].(string)
		if !ok || routeID == "" {
			continue // Skip routes without ID
		}

		// Extract domain using helper function
		domain, ok := extractDomainFromRoute(rawRoute)
		if !ok {
			continue
		}

		// Build Route object with only ID and Domain populated
		route := Route{
			ID:     routeID,
			Domain: domain,
		}
		routes = append(routes, route)
	}

	return routes, nil
}

// RegisterRoutesForAppAndReturn registers routes for an application with Caddy proxy and returns the built routes.
//
// Parameters:
//   - rt: Runtime interface for interacting with pods
//   - appName: Name of the application (e.g., "ai-services" for catalog)
//   - serverName: Caddy server name (e.g., "ai_services")
//   - routesAnnotation: Routes annotation value in format "port:subdomain,port:subdomain,..."
//   - adminURL: Caddy admin API URL (e.g., "http://localhost:37249" or "http://ai-services--caddy:2019")
//   - domainSuffix: Pre-computed domain suffix (e.g., "example.com" or "192.168.1.100.nip.io")
//   - servicePodName: Name of the service pod for upstream configuration
//
// Returns:
//   - []Route: List of successfully built and registered routes
//   - error: nil if routes were registered successfully, error otherwise
func RegisterRoutesForAppAndReturn(
	rt runtime.Runtime,
	appName string,
	serverName string,
	routesAnnotation string,
	adminURL string,
	domainSuffix string,
	servicePodName string,
) ([]Route, error) {
	// Step 1: Create proxy manager with the provided admin URL
	proxyManager := NewCaddyManager(adminURL, serverName)

	// Step 2: Perform health check on Caddy
	if err := proxyManager.HealthCheck(); err != nil {
		return nil, fmt.Errorf(
			"caddy health check failed, routes not registered: %w",
			err,
		)
	}

	// Step 3: Build routes from the annotation string using service pod name for upstreams
	routes, err := BuildRoutesFromAnnotation(routesAnnotation, domainSuffix, servicePodName)
	if err != nil {
		return nil, fmt.Errorf("failed to build routes: %w", err)
	}

	// Step 4: Register each route with Caddy
	var registrationErrors []error
	for _, route := range routes {
		if err := proxyManager.RegisterRoute(route); err != nil {
			registrationErrors = append(registrationErrors, fmt.Errorf("route %s: %w", route.ID, err))
		}
	}

	// Return error if any routes failed to register
	if len(registrationErrors) > 0 {
		return nil, fmt.Errorf("failed to register %d route(s): %w", len(registrationErrors), errors.Join(registrationErrors...))
	}

	return routes, nil
}

// Made with Bob
