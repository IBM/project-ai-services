package proxy

// DefaultHTTPSPort is the standard HTTPS port used as a fallback when
// CADDY_HTTPS_PORT is not set and when building external route URLs.
const DefaultHTTPSPort = "443"

// DefaultPrivateIngressPort is the standard private ingress port used as a fallback.
const DefaultPrivateIngressPort = "9443"

// Names of the Caddy servers that carry private (mTLS) traffic. They must match
// the server names declared in the catalog and worker Caddyfiles, because routes
// are registered on them through the Admin API
// (/config/apps/http/servers/<name>/routes).
const (
	// CaddyIngressServerName is the private mTLS ingress server (inbound VM-to-VM calls).
	CaddyIngressServerName = "private_mtls_ingress"

	// CaddyEgressServerName is the private egress server (plain HTTP in, mTLS out).
	CaddyEgressServerName = "private_mtls_egress"
)

// Caddy-related environment variable names shared across packages.
const (
	// DomainSuffixEnvVar is the env var that holds the domain suffix used
	// when building route hostnames (e.g. "example.com" or "10.0.0.1.nip.io").
	DomainSuffixEnvVar = "DOMAIN_SUFFIX"

	// CaddyHTTPSPortEnvVar is the env var that holds the Caddy HTTPS listener port.
	CaddyHTTPSPortEnvVar = "CADDY_HTTPS_PORT"

	// CaddyPrivateIngressPortEnvVar is the env var that holds the Caddy private ingress listener port.
	CaddyPrivateIngressPortEnvVar = "CADDY_PRIVATE_INGRESS_PORT"

	// CaddyAdminURLEnvVar is the env var that holds the Caddy admin API URL
	// (e.g. "http://ai-services--caddy:2019").
	CaddyAdminURLEnvVar = "CADDY_ADMIN_URL"
)
