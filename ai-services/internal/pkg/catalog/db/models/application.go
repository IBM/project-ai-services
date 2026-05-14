package models

import (
	"time"

	"github.com/google/uuid"
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

// Application represents an application in the catalog.
type Application struct {
	ID        uuid.UUID         `json:"id"`
	Name      string            `json:"name"`
	Template  string            `json:"template"`
	Status    ApplicationStatus `json:"status"`
	Message   string            `json:"message,omitempty"`
	CreatedBy string            `json:"created_by"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Services  []Service         `json:"services,omitempty"`
}
