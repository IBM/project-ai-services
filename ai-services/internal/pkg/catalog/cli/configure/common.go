package configure

// ArgParam keys common to both Podman and OpenShift deployments.
const (
	ArgParamAdminPasswordHash = "backend.adminPasswordHash"
	ArgParamDBPassword        = "db.password"
	ArgParamWorkerGatewayPort = "backend.workerGatewayPort"
)

// ArgParam keys used only by the Podman deployment.
const (
	ArgParamRuntime               = "backend.runtime"
	ArgParamPodmanAuthFileContent = "backend.podman.authFileContent"
	ArgParamPodmanURI             = "backend.podman.uri"
	ArgParamCaddyHTTPSPort        = "caddy.httpsPort"
	ArgParamCaddyFileContent      = "caddy.caddyFileContent"
	ArgParamSSLCertFileContent    = "caddy.sslCertContent"
	ArgParamSSLKeyFileContent     = "caddy.sslKeyContent"
)
