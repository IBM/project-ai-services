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

	// RegisterIngressRoute registers a path-based route on the private mTLS
	// ingress server. Requests under PathPrefix have the prefix stripped and are
	// reverse-proxied to Upstream. Registering an existing ID is a no-op.
	// Remove it with UnregisterRoute.
	RegisterIngressRoute(ctx context.Context, route IngressRoute) error

	// RegisterEgressRoute registers a route on the private egress server that
	// accepts plain HTTP locally and forwards it to the remote private ingress
	// over mTLS. Registering an existing ID is a no-op.
	// Remove it with UnregisterRoute.
	RegisterEgressRoute(ctx context.Context, route EgressRoute) error
}

// IngressRoute is an inbound route on the private mTLS ingress server.
type IngressRoute struct {
	// ID is the unique identifier for the route (used as the Caddy @id)
	ID string

	// PathPrefix is the URL path prefix to match (e.g., "/models/m1"); it is stripped before proxying
	PathPrefix string

	// Upstream is the backend service address (e.g., "pod-name:8080")
	Upstream string

	// Terminal indicates if route matching should stop after this route
	Terminal bool
}

// EgressRoute is an outbound route on the private egress server: plain HTTP in,
// mTLS out to another node's private ingress.
type EgressRoute struct {
	// ID is the unique identifier for the route (used as the Caddy @id)
	ID string

	// PathPrefix is the URL path prefix to match. It is NOT stripped: the remote
	// ingress route is registered with the same prefix and strips it.
	PathPrefix string

	// DialUpstream is the remote private ingress address (e.g., "10.0.0.2:9443")
	DialUpstream string

	// ClientCertPath and ClientKeyPath are the client identity presented to the
	// remote ingress (plaintext PEM key, readable by Caddy).
	ClientCertPath string
	ClientKeyPath  string

	// TrustedCACertPath is the CA used to verify the remote ingress certificate.
	TrustedCACertPath string

	// ServerName is the TLS server name to verify the remote certificate against.
	// Set it when DialUpstream is an IP or when the dial hostname is not in the
	// remote certificate's SANs.
	ServerName string
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
