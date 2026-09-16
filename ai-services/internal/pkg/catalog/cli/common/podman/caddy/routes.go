package caddy

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

const (
	httpsReadyPollInterval   = 2 * time.Second
	httpsReadyTimeout        = 60 * time.Second
	httpsReadyRequestTimeout = 5 * time.Second
)

// WaitForTLSReady attempts a TLS handshake against rawURL until it succeeds,
// indicating that Caddy has finished its TLS reload for the domain.
//
// It uses tls.DialWithDialer to perform only the TLS handshake without sending
// application HTTP requests or hitting backend services.
// Certificate verification is skipped because the catalog may use a self-signed
// certificate; the goal is solely to confirm the TLS handshake completes without
// a fatal alert.
func WaitForTLSReady(ctx context.Context, rawURL string) error {
	logger.Debugf("Waiting for Caddy TLS to be ready at %s...\n", rawURL)

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL %s: %w", rawURL, err)
	}

	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	addr := net.JoinHostPort(host, port)

	dialer := &net.Dialer{Timeout: httpsReadyRequestTimeout}
	tlsConfig := &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true, //nolint:gosec // self-signed certs are expected
	}

	deadline := time.Now().Add(httpsReadyTimeout)
	ticker := time.NewTicker(httpsReadyPollInterval)
	defer ticker.Stop()

	for {
		conn, dialErr := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
		if dialErr == nil {
			_ = conn.Close()
			logger.Debugf("Caddy TLS ready at %s\n", addr)

			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for Caddy TLS at %s: %w", addr, dialErr)
		}

		logger.Debugf("Caddy TLS not ready at %s (%v), retrying in %s...\n", addr, dialErr, httpsReadyPollInterval)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

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

	// Ensure all registered routes have their TLS layer ready before returning.
	// Route registration triggers a Caddy config reload that briefly interrupts TLS.
	for _, routeURL := range routeDomains {
		if err := WaitForTLSReady(ctx, routeURL); err != nil {
			return nil, fmt.Errorf("TLS readiness check failed for %s: %w", routeURL, err)
		}
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
// a standardised key derived from the subdomain (e.g. "CATALOG_API_ROUTE").
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
// to the routeURLs map under a standardised key (e.g. "CATALOG_API_ROUTE").
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
