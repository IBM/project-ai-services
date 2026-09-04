package proxy

import "context"

// ProxyManager defines the interface for managing reverse proxy routes.
type ProxyManager interface {
	// RegisterRoute registers a new route with the proxy and returns the
	// fully-qualified external URL for the route.
	RegisterRoute(ctx context.Context, route Route) (string, error)

	// UnregisterRoute removes a route from the proxy by its ID
	UnregisterRoute(ctx context.Context, routeID string) error

	// HealthCheck verifies the proxy is available and responding
	HealthCheck(ctx context.Context) error

	// GetRouteByID retrieves a specific route by its ID from the proxy
	GetRouteByID(ctx context.Context, routeID string) (*Route, error)
}

// Route represents a reverse proxy route configuration.
type Route struct {
	// ID is the unique identifier for the route
	ID string

	// Domain is the hostname to match (e.g., "service.example.com")
	Domain string

	// Upstream is the backend service address (e.g., "pod-name:8080")
	Upstream string

	// Terminal indicates if route matching should stop after this route
	Terminal bool

	// Type indicates the endpoint type
	Type string

	// ExternalURL is the fully-qualified HTTPS URL for this route
	// (e.g., "https://service.example.com" or "https://service.example.com:8443").
	// Populated by RegisterRoutesForAppAndReturn; empty when routes are built
	// without a known HTTPS port.
	ExternalURL string
}

// Made with Bob
