package proxy

// DefaultHTTPSPort is the standard HTTPS port used as a fallback when
// CADDY_HTTPS_PORT is not set and when building external route URLs.
const DefaultHTTPSPort = "443"

// Caddy-related environment variable names shared across packages.
const (
	// DomainSuffixEnvVar is the env var that holds the domain suffix used
	// when building route hostnames (e.g. "example.com" or "10.0.0.1.nip.io").
	DomainSuffixEnvVar = "DOMAIN_SUFFIX"

	// CaddyHTTPSPortEnvVar is the env var that holds the Caddy HTTPS listener port.
	CaddyHTTPSPortEnvVar = "CADDY_HTTPS_PORT"

	// CaddyAdminURLEnvVar is the env var that holds the Caddy admin API URL
	// (e.g. "http://ai-services--caddy:2019").
	CaddyAdminURLEnvVar = "CADDY_ADMIN_URL"
)
