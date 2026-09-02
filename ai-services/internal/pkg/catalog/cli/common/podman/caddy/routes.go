package caddy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
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
func RegisterCatalogRoutes(ctx context.Context, runtime *podman.PodmanClient, caddyCtx *Context, routeInfos []TemplateRouteInfo) (map[string]string, error) {
	if len(routeInfos) == 0 {
		logger.Infof("No templates found with routes annotation, skipping route registration\n")

		return nil, nil
	}

	// Create proxy manager using Caddy context
	proxyManager, err := caddyCtx.CreateProxyManager(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy manager: %w", err)
	}

	// DOMAIN_SUFFIX and CADDY_HTTPS_PORT are not
	// set in the process environment. Set them now so RegisterRoute can build
	// the correct ExternalURL from them.
	httpsPort, err := caddyCtx.GetHTTPSPort(ctx, runtime)
	if err != nil {
		return nil, fmt.Errorf("failed to get Caddy HTTPS port: %w", err)
	}

	_ = os.Setenv(proxy.DomainSuffixEnvVar, caddyCtx.GetDomainSuffix())
	_ = os.Setenv(proxy.CaddyHTTPSPortEnvVar, httpsPort)

	// Build route domains map
	routeDomains := make(map[string]string)

	// Register routes for each template that has them
	var registrationErrors []error
	for _, info := range routeInfos {
		logger.Debugf("Registering routes for pod: %s\n", info.PodName)

		// Register routes and get the built routes back
		routes, err := proxy.RegisterRoutesForAppAndReturn(ctx, constants.CatalogAppName, proxyManager, info.RoutesAnnotation, info.PodName)
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

// GetCatalogRouteInfo retrieves route for the catalog service by querying
// Caddy for existing routes.
func GetCatalogRouteInfo(ctx context.Context, caddyCtx *Context, runtime *podman.PodmanClient, routeInfos []TemplateRouteInfo) (map[string]string, error) {
	proxyManager, err := caddyCtx.CreateProxyManager(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy manager: %w", err)
	}

	// Set CADDY_HTTPS_PORT from the live Caddy pod so GetRouteByID can build
	// the correct ExternalURL even when called outside of configure (where the
	// env var was not pre-set by RegisterCatalogRoutes).
	httpsPort, err := caddyCtx.GetHTTPSPort(ctx, runtime)
	if err != nil {
		return nil, fmt.Errorf("failed to get Caddy HTTPS port: %w", err)
	}

	_ = os.Setenv(proxy.CaddyHTTPSPortEnvVar, httpsPort)

	routeURLs := make(map[string]string)
	for _, info := range routeInfos {
		processRouteInfo(ctx, info, proxyManager, routeURLs)
	}

	return routeURLs, nil
}

// Helper functions for route processing

// createRouteVariableName creates a standardized key from a subdomain.
// Converts "catalog-ui" to "CATALOG_UI_ROUTE".
func createRouteVariableName(subdomain string) string {
	sanitized := strings.ReplaceAll(subdomain, "-", "_")

	return strings.ToUpper(fmt.Sprintf("%s_ROUTE", sanitized))
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

// addRoutesToDomainMap stores each route's ExternalURL in the domain map under
// a standardised key derived from the subdomain (e.g. "CATALOG_API_URL").
// The full URL is used directly so callers don't need a separate HTTPS port.
func addRoutesToDomainMap(routes []proxy.Route, routeDomains map[string]string) {
	for _, route := range routes {
		if route.ExternalURL == "" {
			continue
		}

		parsed, err := url.Parse(route.ExternalURL)
		if err != nil || parsed.Hostname() == "" {
			continue
		}

		subdomain := extractSubdomainFromDomain(parsed.Hostname())
		if subdomain != "" {
			varName := createRouteVariableName(subdomain)
			routeDomains[varName] = route.ExternalURL
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

// processRouteInfo queries Caddy for each route and adds its ExternalURL
// to the routeURLs map under a standardised key (e.g. "CATALOG_API_URL").
// ExternalURL is now populated by GetRouteByID directly.
func processRouteInfo(ctx context.Context, info TemplateRouteInfo, proxyManager proxy.ProxyManager, routeURLs map[string]string) {
	for _, routeEntry := range strings.Split(info.RoutesAnnotation, ",") {
		subdomain := parseRouteEntry(strings.TrimSpace(routeEntry), info.PodName)
		if subdomain == "" {
			continue
		}

		// Query Caddy for this route (route ID is the subdomain)
		actualRoute, err := proxyManager.GetRouteByID(ctx, subdomain)
		if err != nil {
			// Log warning but continue - route might not exist yet
			logger.Warningf("Failed to query route %s from Caddy: %v", subdomain, err)

			continue
		}

		// Use standardized variable name creation
		varName := createRouteVariableName(subdomain)
		routeURLs[varName] = actualRoute.ExternalURL
	}
}

// Made with Bob
