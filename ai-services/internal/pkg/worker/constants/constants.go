// Package constants holds constants shared across the worker sub-packages
// (deploy, join, uninstall, gateway, etc.) to avoid duplication.
package constants

const (
	// WorkerProxyLabel is the pod label set by deploy.Setup; used by deploy (idempotency) and uninstall (lookup).
	WorkerProxyLabel = "ai-services.io/component=worker-proxy"

	// WorkerDataSubDir is the on-disk subtree written by deploy.Setup; removed by uninstall.
	WorkerDataSubDir = "worker"

	// BaseDirEnvVar is injected into the Caddy container at deploy time; read back by uninstall.
	BaseDirEnvVar = "AI_SERVICES_BASE_DIR"

	// MetaKeyBaseDir, MetaKeyDomainSuffix, MetaKeyHTTPSPort are RegisterRequest.Metadata keys sent during join.
	MetaKeyBaseDir      = "basedir"
	MetaKeyDomainSuffix = "domainSuffix"
	MetaKeyHTTPSPort    = "httpsPort"
)
