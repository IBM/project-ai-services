package caddy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

// TemplateRouteInfo contains route information extracted from a template.
type TemplateRouteInfo struct {
	PodName          string
	RoutesAnnotation string
}

// RegisterCatalogRoutes registers routes with Caddy and returns route domains.
// Accepts pre-extracted route infos from templates.
func RegisterCatalogRoutes(runtime *podman.PodmanClient, caddyCtx *Context, routeInfos []TemplateRouteInfo) (map[string]string, error) {
	if len(routeInfos) == 0 {
		logger.Infof("No templates found with routes annotation, skipping route registration\n")

		return nil, nil
	}

	// Create proxy manager using Caddy context
	proxyManager, err := caddyCtx.CreateProxyManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy manager: %w", err)
	}

	// Build route domains map
	routeDomains := make(map[string]string)

	// Register routes for each template that has them
	var registrationErrors []error
	for _, info := range routeInfos {
		logger.Debugf("Registering routes for pod: %s\n", info.PodName)

		// Register routes and get the built routes back
		routes, err := proxy.RegisterRoutesForAppAndReturn(context.Background(), constants.CatalogAppName, proxyManager, info.RoutesAnnotation, caddyCtx.GetDomainSuffix(), info.PodName)
		if err != nil {
			registrationErrors = append(registrationErrors, fmt.Errorf("pod %s: %w", info.PodName, err))

			continue
		}

		addRoutesToDomainMap(routes, routeDomains)
	}

	// Return error if any routes failed to register
	if len(registrationErrors) > 0 {
		return nil, fmt.Errorf("failed to register routes for %d pod(s): %w", len(registrationErrors), errors.Join(registrationErrors...))
	}

	logger.Infof("Successfully registered routes for %d pod(s)\n", len(routeInfos))

	return routeDomains, nil
}

// GetCatalogRouteInfo retrieves route domains and HTTPS port for the catalog service.
// Accepts pre-extracted route infos from templates.
func GetCatalogRouteInfo(caddyCtx *Context, runtime *podman.PodmanClient, routeInfos []TemplateRouteInfo) (map[string]string, string, error) {
	// Create proxy manager
	proxyManager, err := caddyCtx.CreateProxyManager()
	if err != nil {
		return nil, "", fmt.Errorf("failed to create proxy manager: %w", err)
	}

	// Build route domains map by querying Caddy
	routeDomains := make(map[string]string)
	for _, info := range routeInfos {
		processRouteInfo(info, proxyManager, routeDomains)
	}

	// Get Caddy HTTPS port
	httpsPort, err := caddyCtx.GetHTTPSPort(runtime)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get Caddy HTTPS port: %w", err)
	}

	return routeDomains, httpsPort, nil
}

// Helper functions for route processing

// createRouteVariableName creates a standardized environment variable name from a subdomain.
// Converts "catalog-ui" to "CATALOG_UI_DOMAIN".
func createRouteVariableName(subdomain string) string {
	sanitized := strings.ReplaceAll(subdomain, "-", "_")

	return strings.ToUpper(fmt.Sprintf("%s_DOMAIN", sanitized))
}

// extractSubdomainFromDomain extracts the subdomain from a full domain.
// For "catalog-ui.example.com", returns "catalog-ui".
func extractSubdomainFromDomain(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) > 0 {
		return parts[0]
	}

	return ""
}

// addRoutesToDomainMap adds routes to the domain map with standardized variable names.
func addRoutesToDomainMap(routes []proxy.Route, routeDomains map[string]string) {
	for _, route := range routes {
		subdomain := extractSubdomainFromDomain(route.Domain)
		if subdomain != "" {
			varName := createRouteVariableName(subdomain)
			routeDomains[varName] = route.Domain
		}
	}
}

// parseRouteEntry parses a single route entry and returns the subdomain.
// Route format: "port:subdomain:type"
// Returns empty string if the entry is invalid.
func parseRouteEntry(routeEntry, podName string) string {
	parts, err := proxy.ParseRouteEntry(routeEntry)
	if err != nil {
		logger.Warningf("Invalid route format '%s' in pod %s: %v", routeEntry, podName, err)

		return ""
	}

	return parts.Subdomain
}

// processRouteInfo processes route information and populates the routeDomains map.
// Queries Caddy for each route and adds it to the map with a standardized variable name.
func processRouteInfo(info TemplateRouteInfo, proxyManager proxy.ProxyManager, routeDomains map[string]string) {
	// Parse routes annotation to extract subdomains
	// Format: "port:subdomain:type, port:subdomain:type, ..."
	// Example: "8081:catalog-ui:ui, 8080:catalog-api:api"
	for _, routeEntry := range strings.Split(info.RoutesAnnotation, ",") {
		subdomain := parseRouteEntry(strings.TrimSpace(routeEntry), info.PodName)
		if subdomain == "" {
			continue
		}

		// Query Caddy for this route (route ID is the subdomain)
		actualRoute, err := proxyManager.GetRouteByID(subdomain)
		if err != nil {
			// Log warning but continue - route might not exist yet
			logger.Warningf("Failed to query route %s from Caddy: %v", subdomain, err)

			continue
		}

		// Use standardized variable name creation
		varName := createRouteVariableName(subdomain)
		routeDomains[varName] = actualRoute.Domain
	}
}

// Made with Bob
