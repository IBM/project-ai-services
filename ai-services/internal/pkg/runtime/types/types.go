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
	HealthcheckStartPeriod time.Duration
}

type Image struct {
	RepoTags    []string
	RepoDigests []string
}
