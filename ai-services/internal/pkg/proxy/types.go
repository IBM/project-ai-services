package proxy

import "time"

// Route represents a service route configuration
type Route struct {
	ID              string // Unique route identifier (e.g., "route-ai-services--catalog-ui")
	Host            string // Domain name (e.g., "ai-services--catalog-ui.example.com")
	UpstreamAddress string // Backend service address (e.g., "ai-services--catalog:8081")
	Terminal        bool   // Stop route matching after this route
}

// TLSConfig represents TLS configuration
type TLSConfig struct {
	CertPath string // Path to certificate file
	KeyPath  string // Path to private key file
	Domain   string // Domain name extracted from certificate
}

// CaddyConfig represents Caddy server configuration
type CaddyConfig struct {
	AdminURL       string        // Caddy Admin API URL (e.g., "http://127.0.0.1:2019")
	ServerName     string        // Server name in Caddy config
	HTTPSPort      int           // HTTPS port (default: 443)
	RequestTimeout time.Duration // HTTP request timeout
}

// RouteRegistrationResult contains the result of route registration
type RouteRegistrationResult struct {
	RouteID string
	URL     string
	Success bool
	Error   error
}

// Made with Bob
