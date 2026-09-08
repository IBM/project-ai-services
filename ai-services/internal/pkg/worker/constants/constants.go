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
	// WorkerJoinErr is the error log message emitted by the worker container when it fails to establish a gRPC stream connection to the gateway.
	WorkerJoinErr = "failed to join the worker"
)


{"ts":1788869305805.001,"caller":"join/join.go:113","msg":"worker join: ca.crt not present, bootstrap connection will use InsecureSkipVerify","v":0,"level":"WARNING","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:113"}
{"ts":1788869305805.0852,"caller":"join/join.go:123","msg":"worker join: registering with catalog control plane...","v":0,"level":"INFO","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:123"}
Error: worker join: register: worker join: register RPC: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 10.20.179.49:9090: connect: connection refused"
{"ts":1788869305988.186,"caller":"join/join.go:97","msg":"Registering worker with catalog control plane...","v":0,"level":"INFO","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:97"}
{"ts":1788869305988.68,"caller":"join/join.go:113","msg":"worker join: ca.crt not present, bootstrap connection will use InsecureSkipVerify","v":0,"level":"WARNING","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:113"}
{"ts":1788869305988.7354,"caller":"join/join.go:123","msg":"worker join: registering with catalog control plane...","v":0,"level":"INFO","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:123"}
Error: worker join: register: worker join: register RPC: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 10.20.179.49:9090: connect: connection refused"
{"ts":1788869306182.977,"caller":"join/join.go:97","msg":"Registering worker with catalog control plane...","v":0,"level":"INFO","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:97"}
{"ts":1788869306183.4849,"caller":"join/join.go:113","msg":"worker join: ca.crt not present, bootstrap connection will use InsecureSkipVerify","v":0,"level":"WARNING","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:113"}
{"ts":1788869306183.5417,"caller":"join/join.go:123","msg":"worker join: registering with catalog control plane...","v":0,"level":"INFO","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:123"}
Error: worker join: register: worker join: register RPC: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 10.20.179.49:9090: connect: connection refused"
{"ts":1788869306375.039,"caller":"join/join.go:97","msg":"Registering worker with catalog control plane...","v":0,"level":"INFO","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:97"}
{"ts":1788869306375.5383,"caller":"join/join.go:113","msg":"worker join: ca.crt not present, bootstrap connection will use InsecureSkipVerify","v":0,"level":"WARNING","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:113"}
{"ts":1788869306375.6025,"caller":"join/join.go:123","msg":"worker join: registering with catalog control plane...","v":0,"level":"INFO","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:123"}
Error: worker join: register: worker join: register RPC: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 10.20.179.49:9090: connect: connection refused"
{"ts":1788869306559.5686,"caller":"join/join.go:97","msg":"Registering worker with catalog control plane...","v":0,"level":"INFO","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:97"}
{"ts":1788869306560.0781,"caller":"join/join.go:113","msg":"worker join: ca.crt not present, bootstrap connection will use InsecureSkipVerify","v":0,"level":"WARNING","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:113"}
{"ts":1788869306560.147,"caller":"join/join.go:123","msg":"worker join: registering with catalog control plane...","v":0,"level":"INFO","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:123"}
Error: worker join: register: worker join: register RPC: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 10.20.179.49:9090: connect: connection refused"
{"ts":1788869306769.2773,"caller":"join/join.go:97","msg":"Registering worker with catalog control plane...","v":0,"level":"INFO","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:97"}
{"ts":1788869306769.807,"caller":"join/join.go:113","msg":"worker join: ca.crt not present, bootstrap connection will use InsecureSkipVerify","v":0,"level":"WARNING","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:113"}
{"ts":1788869306769.9236,"caller":"join/join.go:123","msg":"worker join: registering with catalog control plane...","v":0,"level":"INFO","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:123"}
Error: worker join: register: worker join: register RPC: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 10.20.179.49:9090: connect: connection refused"
{"ts":1788869306942.6416,"caller":"join/join.go:97","msg":"Registering worker with catalog control plane...","v":0,"level":"INFO","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:97"}
{"ts":1788869306943.263,"caller":"join/join.go:113","msg":"worker join: ca.crt not present, bootstrap connection will use InsecureSkipVerify","v":0,"level":"WARNING","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:113"}
{"ts":1788869306943.338,"caller":"join/join.go:123","msg":"worker join: registering with catalog control plane...","v":0,"level":"INFO","caller_fullpath":"/go/src/github.com/project-ai-services/ai-services/internal/pkg/worker/join/join.go:123"}
Error: worker join: register: worker join: register RPC: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp 10.20.179.49:9090: connect: connection refused"