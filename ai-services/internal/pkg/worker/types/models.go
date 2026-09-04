package types

// WorkerConnectionOptions carries the gateway address and bootstrap token
// shared by all worker join variants.
type WorkerConnectionOptions struct {
	// GatewayAddr is the host:port of the catalog gRPC worker-gateway,
	// e.g. "catalog.example.com:9090".
	GatewayAddr string

	// Token is the single-use bootstrap token issued by
	// `ai-services catalog worker register`.
	Token string
}

// PodmanWorkerOptions carries everything needed to join a worker to the catalog control plane.
type PodmanWorkerOptions struct {
	WorkerConnectionOptions

	// Setup holds the options for setting up this worker node (Caddy proxy,
	// model storage, etc.). Setup runs before the gRPC handshake so the
	// worker is ready to serve routes as soon as it connects.
	Setup Options
}

// OpenshiftWorkerOptions carries everything needed to join a worker to the catalog control plane.
type OpenshiftWorkerOptions struct {
	WorkerConnectionOptions
}

// GrpcStreamOptions carries everything needed to join a worker to the catalog control plane.
type GrpcStreamOptions struct {
	WorkerConnectionOptions

	// RequestedWorkerName is forwarded in the RegisterRequest.worker_name field.
	// For normal (token-authenticated) workers this is empty, the gateway derives
	// the name from the token. Set to workerconstants.LocalWorkerName when joining
	// the catalog host as the local worker so the gateway applies the local-bypass path.
	RequestedWorkerName string
}

// Options carries the parameters needed to set up the worker node.
type Options struct {
	// BaseDir is the host directory used for worker data / config volumes
	// (Caddy config, models, etc.).
	// Set once at first join; ignored on subsequent runs if already deployed.
	BaseDir string
	// HTTPSPort is the host port Caddy binds for HTTPS traffic, e.g. "443".
	// Set once at first join; ignored on subsequent runs if already deployed.
	HTTPSPort int

	// DomainName is an optional custom domain suffix for self-signed certificates.
	// If empty, Caddy uses wildcard DNS format: <service>.<ip>.nip.io.
	DomainName string
	// SSLCertPath is the path to a user-provided SSL certificate (PEM).
	// Must be used together with SSLKeyPath.
	SSLCertPath string
	// SSLKeyPath is the path to a user-provided SSL private key (PEM).
	// Must be used together with SSLCertPath.
	SSLKeyPath string
}
