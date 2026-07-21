package configure

// ArgParam keys common to both Podman and OpenShift deployments.
const (
	ArgParamAdminPasswordHash = "backend.adminPasswordHash"
	ArgParamDBPassword        = "db.password"
)

// ArgParam keys used only by the OpenShift deployment.
// (no OpenShift-specific params at present; extend here when needed)

// ArgParam keys used only by the Podman deployment.
const (
	ArgParamRuntime               = "backend.runtime"
	ArgParamPodmanAuthFileContent = "backend.podman.authFileContent"
	ArgParamPodmanURI             = "backend.podman.uri"
	ArgParamCaddyHTTPSPort        = "caddy.httpsPort"
)
