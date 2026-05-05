// Package models defines the data models for the catalog database.
package models

import (
	"time"

	"github.com/google/uuid"
)

// ApplicationStatus represents the status of an application.
type ApplicationStatus string

const (
	// ApplicationStatusPending indicates the application is pending deployment.
	ApplicationStatusPending ApplicationStatus = "pending"
	// ApplicationStatusRunning indicates the application is running.
	ApplicationStatusRunning ApplicationStatus = "running"
	// ApplicationStatusStopped indicates the application is stopped.
	ApplicationStatusStopped ApplicationStatus = "stopped"
	// ApplicationStatusFailed indicates the application deployment failed.
	ApplicationStatusFailed ApplicationStatus = "failed"
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

// Service represents a service associated with an application.
type Service struct {
	ID        uuid.UUID         `json:"id"`
	AppID     uuid.UUID         `json:"app_id"`
	Type      string            `json:"type"`
	Status    ApplicationStatus `json:"status"`
	Endpoints map[string]any    `json:"endpoints,omitempty"`
	Version   string            `json:"version,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Made with Bob
