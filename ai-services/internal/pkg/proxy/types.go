package proxy

// ProxyManager defines the interface for managing reverse proxy routes.
type ProxyManager interface {
	// RegisterRoute registers a new route with the proxy
	RegisterRoute(route Route) error

	// HealthCheck verifies the proxy is available and responding
	HealthCheck() error
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
}

// Made with Bob
