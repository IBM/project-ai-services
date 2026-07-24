package types

import "time"

// RuntimeType represents the type of container runtime.
type RuntimeType string

const (
	RuntimeTypePodman    RuntimeType = "podman"
	RuntimeTypeOpenShift RuntimeType = "openshift"
)

// String returns the string representation of RuntimeType.
func (r RuntimeType) String() string {
	return string(r)
}

// Valid checks if the runtime type is valid.
func (r RuntimeType) Valid() bool {
	switch r {
	case RuntimeTypePodman, RuntimeTypeOpenShift:
		return true
	default:
		return false
	}
}

type Pod struct {
	ID               string
	Name             string
	Status           string
	Health           string
	Labels           map[string]string
	Containers       []Container
	Created          time.Time
	Ports            map[string][]string
	State            string
	InfraContainerID string
}

type Container struct {
	ID                     string `json:"ID"`
	Name                   string
	Status                 string
	Health                 string
	Annotations            map[string]string
	Env                    map[string]string
	HealthcheckStartPeriod time.Duration
}

type Image struct {
	RepoTags    []string
	RepoDigests []string
}

type Route struct {
	Name       string
	HostPort   string
	TargetPort string
}

// DeploymentStatus represents the observed status of a Kubernetes/OpenShift Deployment.
type DeploymentStatus struct {
	// DesiredReplicas is the number of desired pod replicas specified in the spec.
	DesiredReplicas int32
	// ReadyReplicas is the number of pods that have passed their readiness checks.
	ReadyReplicas int32
	// AvailableReplicas is the number of pods available to serve requests.
	AvailableReplicas int32
	// UpdatedReplicas is the number of pods updated to the latest version.
	UpdatedReplicas int32
	// UnavailableReplicas is the number of pods that are not yet available.
	UnavailableReplicas int32
}

// PodResources represents resource allocation and usage for a pod including accelerators.
type PodResources struct {
	CPU        float64  // CPU usage (e.g., 1.5 CPUs)
	MemUsage   uint64   // Memory usage in bytes
	SpyreCards []string // List of Spyre card PCI addresses
}
