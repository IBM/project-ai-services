package types

import (
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/image"
)

// CreateOptions contains parameters for creating an application.
type CreateOptions struct {
	// Common
	Name         string
	TemplateName string
	SkipChecks   []string
	ArgParams    map[string]string

	// Podman
	SkipModelDownload bool
	SkipImageDownload bool
	ValuesFiles       []string
	Values            map[string]any
	ImagePullPolicy   image.ImagePullPolicy
	AutoYes           bool

	// Openshift
	Timeout time.Duration
}

// DeleteOptions contains parameters for deleting an application.
type DeleteOptions struct {
	Name        string
	PodNames    []string
	AutoYes     bool
	SkipCleanup bool
}

// StartOptions contains parameters for starting an application.
type StartOptions struct {
	Name     string
	PodNames []string
	SkipLogs bool
	AutoYes  bool
}

// StopOptions contains parameters for stopping an application.
type StopOptions struct {
	Name     string
	PodNames []string
	AutoYes  bool
}

// ListOptions contains parameters for listing applications.
type ListOptions struct {
	ApplicationName string
	OutputWide      bool
}

// InfoOptions contains parameters for displaying application info.
type InfoOptions struct {
	Name string
}

// LogsOptions contains parameters for displaying application logs.
type LogsOptions struct {
	PodName           string
	ContainerNameOrID string
}

// ApplicationInfo represents information about a deployed application.
type ApplicationInfo struct {
	Name         string
	Template     string
	Version      string
	Pods         []PodInfo
	Status       string
	CreationTime string
}

// PodInfo represents information about a pod.
type PodInfo struct {
	Name       string
	ID         string
	Status     string
	Containers []ContainerInfo
}

// ContainerInfo represents information about a container.
type ContainerInfo struct {
	Name   string
	ID     string
	Status string
	Image  string
}

// Made with Bob
