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

	// CatalogBackendPodLabel is the label key used to identify the catalog-backend pod on OpenShift.
	CatalogBackendPodLabel = "ai-services.io/component"
	// CatalogBackendPodLabelValue is the label value for the catalog-backend pod.
	CatalogBackendPodLabelValue = "catalog-backend"

	// WorkerDataSubDir is the on-disk subtree written by deploy.Setup; removed by uninstall.
	WorkerDataSubDir = "worker"

	WorkerAppName = "ai-services"
	// WorkerAppTemplate is the app name passed to the template provider.
	// Resolves to assets/worker/<runtime>/templates/.
	WorkerAppTemplate     = "worker"
	WorkerHelmReleaseName = "ai-services-worker"
	// WorkerTLSDir is the mount path inside the worker container where mTLS
	// credentials are stored. Backed by the worker-tls Podman PVC; the path is
	// container-internal and must not overlap with the ai-services-data hostPath
	// bind mount. Single source of truth shared between the join, deploy, and
	// uninstall packages.
	WorkerTLSDir = "/data/worker-tls"

	// GatewayPKIDir is the mount path inside the catalog container where gateway
	// PKI files (CA key/cert, server key/cert) are persisted. Backed by the
	// gateway-pki podman PVC. Single source of truth shared between gateway and
	// the catalog pod template.
	GatewayPKIDir = "/data/gateway-pki"

	// WorkerCaddyPodName is the name of the Caddy reverse-proxy pod.
	WorkerCaddyPodName = "ai-services--caddy"

	// BaseDirEnvVar is injected into the Caddy container at deploy time; read back by uninstall.
	BaseDirEnvVar = "AI_SERVICES_BASE_DIR"

	// WorkerMTLSSecretName is the name of the Podman secret that holds the
	// AES-256 mTLS encryption key for the worker node.
	WorkerMTLSSecretName = "worker-mtls-encryption-secret"

	// MetaKeyBaseDir is the worker metadata key sent during Register and stored in worker.metadata JSON.
	MetaKeyBaseDir = "baseDir"

	// WorkerGatewayPort is the default port used by the catalog gRPC worker gateway.
	WorkerGatewayPort = 9090

	// OpenShiftRoutePort is the port used by OpenShift passthrough routes.
	// All OpenShift routes (including the worker-gateway passthrough route) are
	// always reachable on port 443 via the cluster ingress router.
	OpenShiftRoutePort = 443

	// ArgParamWorkerToken, ArgParamWorkerGatewayAddr,
	// ArgParamWorkerPodmanURI, and ArgParamWorkerAuthFile are template
	// value-override keys used when deploying worker pods.
	ArgParamWorkerToken       = "worker.token"
	ArgParamWorkerGatewayAddr = "worker.gatewayAddr"
	ArgParamWorkerPodmanURI   = "worker.podman.uri"
	ArgParamWorkerAuthFile    = "worker.podman.authFileContent"

	// WorkerGatewayName is the DNS name used by the catalog worker gateway route.
	WorkerGatewayName = "catalog-worker-gateway"

	// PodmanGatewayPodName is the Podman catalog pod DNS name embedded in the
	// auto-generated gateway server certificate.
	PodmanGatewayPodName = "ai-services--catalog"

	// OpenShiftCatalogPodName is the pod name prefix used by the catalog-backend
	// Deployment on OpenShift.
	OpenShiftCatalogPodName = "catalog-backend"

	// OpenShiftGatewayServiceEndpoint is the OpenShift service DNS name embedded in the
	// auto-generated gateway server certificate for internal cluster communication.
	OpenShiftGatewayServiceEndpoint = "catalog-api.ai-services.svc.cluster.local"

	// LocalWorkerEnvVar is the environment variable name that enables local-worker mode.
	LocalWorkerEnvVar = "LOCAL_WORKER"

	// LocalWorkerToken is the bootstrap token used for the local
	// self-join. The catalog-backend gateway accepts this token without
	// ValidateToken when LOCAL_WORKER=true.
	LocalWorkerToken = "local-worker"
	// MTLSEncryptionKeyEnv is the environment variable that holds the AES-256 key used to
	// encrypt mTLS private key files at rest (gateway CA key, server key, worker client key).
	// Sourced from the catalog-mtls-encryption-secret Podman/OpenShift secret at runtime.
	MTLSEncryptionKeyEnv = "MTLS_ENCRYPTION_KEY"
)

 const (
	GrpcStreamErr = "failed to start grpc stream"
 )