package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// ErrRouteNotFound is returned when a route is not found in Caddy.
var ErrRouteNotFound = errors.New("route not found")

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

// GetCaddyProxyManager retrieves the Caddy admin URL from environment and creates a ProxyManager.
func GetCaddyProxyManager() (ProxyManager, error) {
	adminURL := utils.GetEnv("CADDY_ADMIN_URL", "")
	if adminURL == "" {
		return nil, fmt.Errorf("CADDY_ADMIN_URL environment variable not set")
	}

	return NewCaddyManager(adminURL, constants.CaddyServerName), nil
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

func (c *caddyManager) RegisterRoute(ctx context.Context, route Route) error {
	if route.ID == "" {
		return fmt.Errorf("cannot register route: route ID is empty")
	}

	routeConfig := map[string]any{
		"@id":   route.ID,
		"match": []map[string]any{{"host": []string{route.Domain}}},
		"handle": []map[string]any{{
			"handler":   "reverse_proxy",
			"upstreams": []map[string]any{{"dial": route.Upstream}},
		}},
		"terminal": route.Terminal,
	}

	idURL, err := url.JoinPath(c.adminURL, "id", route.ID)
	if err != nil {
		return err
	}

	// Check if route already exists
	checkResp, err := c.httpClient.R().Get(idURL)
	if err != nil {
		return fmt.Errorf("failed to check route existence: %w", err)
	}

	if checkResp.StatusCode() == http.StatusOK {
		// Route already exists, skip registration
		logger.DebugfCtx(ctx, "Route %s already exists, skipping registration\n", route.ID)

		return nil
	}

	// Route doesn't exist, create it
	return c.createRoute(routeConfig)
}

// Helper to append a new route to the server's route array.
func (c *caddyManager) createRoute(routeConfig map[string]any) error {
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
// Returns the domain or an error if extraction fails.
func extractDomainFromRoute(rawRoute map[string]any) (string, error) {
	matches, ok := rawRoute["match"].([]any)
	if !ok || len(matches) == 0 {
		return "", errors.New("missing or empty 'match' field in route")
	}

	firstMatch, ok := matches[0].(map[string]any)
	if !ok {
		return "", errors.New("invalid match format in route")
	}

	hosts, ok := firstMatch["host"].([]any)
	if !ok || len(hosts) == 0 {
		return "", errors.New("missing or empty 'host' field in route")
	}

	domain, ok := hosts[0].(string)
	if !ok || domain == "" {
		return "", errors.New("invalid or empty domain in route")
	}

	return domain, nil
}

// GetRouteByID retrieves a specific route by its ID from Caddy.
func (c *caddyManager) GetRouteByID(routeID string) (*Route, error) {
	idURL, err := url.JoinPath(c.adminURL, "id", routeID)
	if err != nil {
		return nil, fmt.Errorf("failed to build route ID URL: %w", err)
	}

	var rawRoute map[string]any
	resp, err := c.httpClient.R().
		SetResult(&rawRoute).
		Get(idURL)
	if err != nil {
		return nil, fmt.Errorf("failed to query route %s: %w", routeID, err)
	}

	if resp.StatusCode() == http.StatusNotFound {
		return nil, fmt.Errorf("route %s not found", routeID)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("caddy returned status %d for route %s", resp.StatusCode(), routeID)
	}

	domain, err := extractDomainFromRoute(rawRoute)
	if err != nil {
		return nil, fmt.Errorf("failed to extract domain from route %s: %w", routeID, err)
	}

	return &Route{
		ID:     routeID,
		Domain: domain,
	}, nil
}

// UnregisterRoute removes a route from Caddy by its ID.
// Returns ErrRouteNotFound if the route doesn't exist (404), nil if successfully deleted (200).
func (c *caddyManager) UnregisterRoute(routeID string) error {
	if routeID == "" {
		return fmt.Errorf("route ID cannot be empty")
	}

	idURL, err := url.JoinPath(c.adminURL, "id", routeID)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.R().Delete(idURL)
	if err != nil {
		return fmt.Errorf("failed to unregister route: %w", err)
	}

	// Handle different status codes
	switch resp.StatusCode() {
	case http.StatusOK:
		return nil // Successfully deleted
	case http.StatusNotFound:
		return ErrRouteNotFound // Route doesn't exist
	default:
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode(), resp.String())
	}
}

// RegisterRoutesForAppAndReturn registers routes for an application with Caddy proxy and returns the built routes.
//
// Parameters:
//   - rt: Runtime interface for interacting with pods
//   - appName: Name of the application (e.g., "ai-services" for catalog)
//   - proxyManager: ProxyManager instance for route operations (reuse to avoid creating multiple instances)
//   - routesAnnotation: Routes annotation value in format "port:subdomain,port:subdomain,..."
//   - domainSuffix: Pre-computed domain suffix (e.g., "example.com" or "192.168.1.100.nip.io")
//   - servicePodName: Name of the service pod for upstream configuration
//
// Returns:
//   - []Route: List of successfully built and registered routes
//   - error: nil if routes were registered successfully, error otherwise
func RegisterRoutesForAppAndReturn(
	ctx context.Context,
	appName string,
	proxyManager ProxyManager,
	routesAnnotation string,
	domainSuffix string,
	servicePodName string,
) ([]Route, error) {
	// Step 1: Perform health check on Caddy
	if err := proxyManager.HealthCheck(); err != nil {
		return nil, fmt.Errorf(
			"caddy health check failed, routes not registered: %w",
			err,
		)
	}

	// Step 2: Build routes from the annotation string using service pod name for upstreams
	routes, err := BuildRoutesFromAnnotation(routesAnnotation, domainSuffix, servicePodName)
	if err != nil {
		return nil, fmt.Errorf("failed to build routes: %w", err)
	}

	// Step 3: Register each route with Caddy
	var registrationErrors []error
	for _, route := range routes {
		if err := proxyManager.RegisterRoute(ctx, route); err != nil {
			registrationErrors = append(registrationErrors, fmt.Errorf("route %s: %w", route.ID, err))
		}
	}

	// Return error if any routes failed to register
	if len(registrationErrors) > 0 {
		return nil, fmt.Errorf("failed to register %d route(s): %w", len(registrationErrors), errors.Join(registrationErrors...))
	}

	return routes, nil
}

// UnregisterRoutesFromEndpoints unregisters Caddy routes by reconstructing route IDs from endpoints.
// Uses a map to collect unique route IDs before unregistering (similar to volume/secret deletion pattern).
//
// Parameters:
//   - proxyManager: ProxyManager instance for route operations (reuse to avoid creating multiple instances)
//   - endpoints: List of endpoint objects containing URLs (e.g., [{"type":"ui", "url":"https://digitize-ui-abc123.example.com:443"}])
//   - instanceType: Type of instance (e.g., "service" or "component")
//   - instanceID: ID of the service or component
//
// Returns:
//   - error: nil if all routes were unregistered successfully, error otherwise
func UnregisterRoutesFromEndpoints(
	ctx context.Context,
	proxyManager ProxyManager,
	endpoints []map[string]any,
	instanceType string,
	instanceID string,
) error {
	if len(endpoints) == 0 {
		return nil
	}

	routesToUnregister := extractRouteIDsFromEndpoints(endpoints)

	if len(routesToUnregister) == 0 {
		logger.InfofCtx(ctx, "%s %s: no routes found to unregister", instanceType, instanceID)

		return nil
	}

	logger.InfofCtx(ctx, "Unregistering %d route(s) for %s %s", len(routesToUnregister), instanceType, instanceID)

	return unregisterRoutes(ctx, proxyManager, routesToUnregister, instanceType, instanceID)
}

// extractRouteIDsFromEndpoints extracts unique route IDs from endpoints.
func extractRouteIDsFromEndpoints(endpoints []map[string]any) map[string]bool {
	if len(endpoints) == 0 {
		return nil
	}

	routesToUnregister := make(map[string]bool)

	for _, endpoint := range endpoints {
		urlStr, ok := endpoint["url"].(string)
		if !ok {
			continue
		}

		parsedURL, err := url.Parse(urlStr)
		if err != nil {
			logger.Warningf("Failed to parse endpoint URL %s: %v", urlStr, err)

			continue
		}

		hostname := parsedURL.Hostname()
		parts := strings.Split(hostname, ".")
		if len(parts) == 0 {
			logger.Warningf("Invalid hostname format in URL %s: no subdomain found", urlStr)

			continue
		}

		routeID := parts[0]
		if routeID != "" {
			routesToUnregister[routeID] = true
		}
	}

	return routesToUnregister
}

// unregisterRoutes unregisterroutes and returns error if any fail.
func unregisterRoutes(ctx context.Context, proxyManager ProxyManager, routeIDs map[string]bool, instanceType, instanceID string) error {
	var failedRoutes []string

	for routeID := range routeIDs {
		if err := proxyManager.UnregisterRoute(routeID); err == nil {
			logger.InfofCtx(ctx, "%s %s: Successfully unregistered route: %s", instanceType, instanceID, routeID)
		} else if errors.Is(err, ErrRouteNotFound) {
			logger.InfofCtx(ctx, "%s %s: Route not configured for %s (already unregistered)", instanceType, instanceID, routeID)
		} else {
			logger.ErrorfCtx(ctx, "%s %s: Error unregistering route %s: %v", instanceType, instanceID, routeID, err)
			failedRoutes = append(failedRoutes, routeID)
		}
	}

	if len(failedRoutes) > 0 {
		return fmt.Errorf("failed to unregister %d route(s): %v", len(failedRoutes), failedRoutes)
	}

	return nil
}

// Made with Bob
