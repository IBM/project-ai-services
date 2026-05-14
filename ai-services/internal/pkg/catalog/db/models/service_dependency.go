package models

import (
	"github.com/google/uuid"
)

// DependencyType represents the type of dependency (service or component).
type DependencyType string

const (
	DependencyTypeService   DependencyType = "service"
	DependencyTypeComponent DependencyType = "component"
)

// ServiceDependency represents a dependency relationship between a service and another entity.
// A service can depend on other services or components.
type ServiceDependency struct {
	ServiceID      uuid.UUID      `json:"service_id"`      // The service that has the dependency
	DependencyID   uuid.UUID      `json:"dependency_id"`   // The ID of the dependency (service or component)
	DependencyType DependencyType `json:"dependency_type"` // Type of dependency: "service" or "component"
}

// Made with Bob
