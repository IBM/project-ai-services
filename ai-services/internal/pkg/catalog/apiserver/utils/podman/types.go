package podman

// EndpointInfo holds extracted endpoint information for a deployed pod or container.
type EndpointInfo struct {
	Domain string
	Port   string
}
