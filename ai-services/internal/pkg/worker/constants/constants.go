// Package constants holds constants shared across the worker sub-packages
// (deploy, join, uninstall, gateway, etc.) to avoid duplication.
package constants

const (
	// LocalWorkerName is the sentinel value used when no remote worker is specified.
	// It means "deploy on this machine using the local runtime".
	LocalWorkerName = "Local"

	// WorkerProxyLabel is the pod label set by deploy.Setup; used by deploy (idempotency) and uninstall (lookup).
	WorkerProxyLabel = "ai-services.io/component=proxy"

	// WorkerPodLabel is the pod label set to identify worker pod deployed or not.
	WorkerPodLabel = "ai-services.io/component=worker"

	// WorkerDataSubDir is the on-disk subtree written by deploy.Setup; removed by uninstall.
	WorkerDataSubDir = "worker"

	WorkerAppName = "ai-services"
	// WorkerAppTemplate is the app name passed to the template provider.
	// Resolves to assets/worker/<runtime>/templates/.
	WorkerAppTemplate     = "worker"
	WorkerHelmReleaseName = "ai-services-worker"
	// WorkerTLSDir is the mount path inside the worker container where mTLS
	// credentials are stored. The host-side `worker join` command also writes
	// to this path (outside a container). Single source of truth shared between
	// the join, deploy, and uninstall packages.
	WorkerTLSDir = "/var/lib/ai-services/worker-tls"

	// GatewayPKIDir is the mount path inside the catalog container where gateway
	// PKI files (CA key/cert, server key/cert) are persisted. Backed by the
	// gateway-pki podman PVC. Single source of truth shared between gateway and
	// the catalog pod template.
	GatewayPKIDir = "/var/lib/ai-services/gateway-pki"

	// WorkerCaddyPodName is the name of the Caddy reverse-proxy pod.
	WorkerCaddyPodName = "ai-services--caddy"

	// BaseDirEnvVar is injected into the Caddy container at deploy time; read back by uninstall.
	BaseDirEnvVar = "AI_SERVICES_BASE_DIR"

	// ArgParamCaddyHTTPSPort, ArgParamWorkerToken, ArgParamWorkerGatewayAddr,
	// ArgParamWorkerPodmanURI, and ArgParamWorkerAuthFile are template
	// value-override keys used when deploying worker pods.
	ArgParamCaddyHTTPSPort    = "caddy.httpsPort"
	ArgParamWorkerToken       = "worker.token"
	ArgParamWorkerGatewayAddr = "worker.gatewayAddr"
	ArgParamWorkerPodmanURI   = "worker.podman.uri"
	ArgParamWorkerAuthFile    = "worker.podman.authFileContent"

	// GatewayServerName is the fixed DNS SAN embedded in the auto-generated gateway server
	// certificate. Workers set tls.Config.ServerName to this value so hostname verification
	// succeeds regardless of the IP or public DNS name used to reach the gateway.
	// This is the single source of truth — gateway/pki.go imports this package.
	GatewayServerName = "gateway.ai-services.internal"
)
