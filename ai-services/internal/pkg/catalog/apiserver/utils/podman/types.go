package podman

// EndpointInfo holds extracted endpoint information for a deployed pod or container.
type EndpointInfo struct {
	Endpoint string
	Domain   string
	Port     string
}
