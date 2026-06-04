package models

import (
	"time"

	"github.com/google/uuid"
)

// DeploymentType represents the deployment type of an application.
type DeploymentType string

const (
	DeploymentTypeArchitectures DeploymentType = "architectures"
	DeploymentTypeServices      DeploymentType = "services"
)

// ApplicationStatus represents the status of an application.
type ApplicationStatus string

const (
	ApplicationStatusDownloading ApplicationStatus = "Downloading"
	ApplicationStatusDeploying   ApplicationStatus = "Deploying"
	ApplicationStatusRunning     ApplicationStatus = "Running"
	ApplicationStatusDeleting    ApplicationStatus = "Deleting"
	ApplicationStatusError       ApplicationStatus = "Error"
)

// ServiceStatus represents the status of a service.
type ServiceStatus string

const (
	ServiceStatusInitializing ServiceStatus = "Initializing"
	ServiceStatusRunning      ServiceStatus = "Running"
	ServiceStatusError        ServiceStatus = "Error"
)

// ComponentStatus represents the status of a component.
type ComponentStatus string

const (
	ComponentStatusInitializing ComponentStatus = "Initializing"
	ComponentStatusRunning      ComponentStatus = "Running"
	ComponentStatusError        ComponentStatus = "Error"
)

// Application represents an application in the catalog.
type Application struct {
	ID             uuid.UUID         `json:"id"`
	Name           string            `json:"name"`
	CatalogID      string            `json:"catalog_id"`
	DeploymentType DeploymentType    `json:"deployment_type"`
	Status         ApplicationStatus `json:"status"`
	Message        string            `json:"message,omitempty"`
	Version        string            `json:"version"`
	CreatedBy      string            `json:"created_by"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	Services       []Service         `json:"services,omitempty"`
}
