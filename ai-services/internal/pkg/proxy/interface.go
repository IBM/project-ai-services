package proxy

// ProxyManager defines the interface for managing reverse proxy routes
type ProxyManager interface {
	// RegisterRoute registers a new route with the proxy
	RegisterRoute(route Route) error

	// UnregisterRoute removes a route from the proxy
	UnregisterRoute(routeID string) error

	// LoadCertificates loads user-provided certificates into the proxy
	LoadCertificates(config TLSConfig) error

	// HealthCheck verifies the proxy is available and responding
	HealthCheck() error

	// GetRoutes retrieves all registered routes
	GetRoutes() ([]Route, error)
}

// Made with Bob
