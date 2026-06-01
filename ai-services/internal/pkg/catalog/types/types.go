package types

// Architecture represents a complete AI solution template.
type Architecture struct {
	ID               string               `yaml:"id" json:"id"`
	Name             string               `yaml:"name" json:"name"`
	Description      string               `yaml:"description" json:"description"`
	Version          string               `yaml:"version" json:"version"`
	Type             string               `yaml:"type" json:"type"` // "architecture"
	CertifiedBy      string               `yaml:"certified_by" json:"certified_by"`
	Runtimes         []string             `yaml:"runtimes" json:"runtimes"`
	GlobalComponents []ComponentReference `yaml:"global_components,omitempty" json:"global_components,omitempty"`
	Services         []ServiceReference   `yaml:"services" json:"services"`
	Links            *ArchitectureLinks   `yaml:"links,omitempty" json:"links,omitempty"`
}

// ArchitectureSummary represents an architecture for list API responses.
type ArchitectureSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	CertifiedBy string   `json:"certified_by"`
	Services    []string `json:"services"`
}

// ArchitectureLinks contains links related to an architecture.
type ArchitectureLinks struct {
	Demo          string `yaml:"demo,omitempty" json:"demo,omitempty"`
	Code          string `yaml:"code,omitempty" json:"code,omitempty"`
	Documentation string `yaml:"documentation,omitempty" json:"documentation,omitempty"`
}

// ServiceReference represents a reference to a service in an architecture.
type ServiceReference struct {
	ID       string `yaml:"id" json:"id"`
	Version  string `yaml:"version,omitempty" json:"version,omitempty"`
	Optional bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

// ComponentReference represents a reference to a component type.
type ComponentReference struct {
	Type string `yaml:"type" json:"type"`
}

// DependencyReference represents a reference to a dependency service.
type DependencyReference struct {
	ID string `yaml:"id" json:"id"`
}

// Service represents a deployable AI service.
type Service struct {
	ID            string                `yaml:"id" json:"id"`
	Name          string                `yaml:"name" json:"name"`
	Description   string                `yaml:"description" json:"description"`
	Type          string                `yaml:"type" json:"type"` // "service"
	CertifiedBy   string                `yaml:"certified_by" json:"certified_by"`
	Architectures []string              `yaml:"architectures" json:"architectures"`
	Dependencies  []DependencyReference `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

// ServiceSummary represents a service for list API responses.
type ServiceSummary struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	CertifiedBy   string   `json:"certified_by"`
	Architectures []string `json:"architectures"`
}

// Component represents an infrastructure component (vector_store, embedding, llm, etc.).
type Component struct {
	ID            string `yaml:"id" json:"id"`
	Name          string `yaml:"name" json:"name"`
	Description   string `yaml:"description" json:"description"`
	Type          string `yaml:"type" json:"type"`                     // "component"
	ComponentType string `yaml:"component_type" json:"component_type"` // "vector_store", "embedding", "llm", etc.
	ComponentName string `yaml:"component_name" json:"component_name"` // Display name for component type (e.g., "Vector Store", "LLM Model")
}

// ComponentSummary represents a component for list API responses.
type ComponentSummary struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	ComponentType string `json:"component_type"`
}

// RuntimeMetadata contains runtime-specific metadata.
type RuntimeMetadata struct {
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version" json:"version"`
}

// DeployOptionsProvider represents a provider for a component type.
type DeployOptionsProvider struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
	Schema      string `json:"schema,omitempty"`
}

// DeployOptionsComponent represents a component type with its providers.
type DeployOptionsComponent struct {
	Type      string                  `json:"type"`
	Name      string                  `json:"name"`
	Providers []DeployOptionsProvider `json:"providers"`
}

// DeployOptionsService represents a service with its components.
// The Type field is optional and used when the service is part of an architecture.
type DeployOptionsService struct {
	Type       string                   `json:"type,omitempty"`
	ID         string                   `json:"id"`
	Name       string                   `json:"name"`
	Schema     string                   `json:"schema,omitempty"`
	Components []DeployOptionsComponent `json:"components"`
}

// DeployOptionsArchitecture represents deploy options for an architecture.
type DeployOptionsArchitecture struct {
	ID               string                   `json:"id"`
	Name             string                   `json:"name"`
	GlobalComponents []DeployOptionsComponent `json:"global_components"`
	Services         []DeployOptionsService   `json:"services"`
}

// Made with Bob
