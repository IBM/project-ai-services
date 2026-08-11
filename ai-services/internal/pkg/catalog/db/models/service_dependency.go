package models

import (
	"github.com/google/uuid"
)

// DependencyType represents the type of dependency (service or component).
type DependencyType string

const (
	DependencyTypeService    DependencyType = "service"
	DependencyTypeComponent  DependencyType = "component"
	DependencyTypeDatasource DependencyType = "datasource"
)

// ServiceDependency represents a dependency relationship between a service and another entity.
// A service can depend on other services, components, or datasource connectors.
type ServiceDependency struct {
	ServiceID      uuid.UUID      `json:"service_id"`      // The service that has the dependency
	DependencyID   uuid.UUID      `json:"dependency_id"`   // The ID of the dependency (service, component, or connector)
	DependencyType DependencyType `json:"dependency_type"` // Type of dependency: "service", "component", or "datasource"
}

// Made with Bob
