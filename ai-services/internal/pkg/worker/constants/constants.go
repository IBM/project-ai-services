// Package constants holds constants shared across the worker sub-packages
// (deploy, join, uninstall, gateway, etc.) to avoid duplication.
package constants

const (
	// LocalWorkerName is the sentinel value used when no remote worker is specified.
	// It means "deploy on this machine using the local runtime".
	LocalWorkerName = "Local"

	// WorkerProxyLabel is the pod label set by deploy.Setup; used by deploy (idempotency) and uninstall (lookup).
	WorkerProxyLabel = "ai-services.io/component=proxy"

	WorkerPodLabel = "ai-services.io/component=worker"
	WorkerTokenSecretName = "worker-token-secret"

	// WorkerDataSubDir is the on-disk subtree written by deploy.Setup; removed by uninstall.
	WorkerDataSubDir = "worker"

	// BaseDirEnvVar is injected into the Caddy container at deploy time; read back by uninstall.
	BaseDirEnvVar = "AI_SERVICES_BASE_DIR"

	// MetaKeyBaseDir, MetaKeyDomainSuffix, MetaKeyHTTPSPort are RegisterRequest.Metadata keys sent during join.
	MetaKeyBaseDir      = "basedir"
	MetaKeyDomainSuffix = "domainSuffix"
	MetaKeyHTTPSPort    = "httpsPort"
)
