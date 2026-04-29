package types

// Architecture represents a complete AI solution template
type Architecture struct {
	ID          string             `yaml:"id" json:"id"`
	Name        string             `yaml:"name" json:"name"`
	Description string             `yaml:"description" json:"description"`
	Version     string             `yaml:"version" json:"version"`
	Type        string             `yaml:"type" json:"type"` // "architecture"
	CertifiedBy string             `yaml:"certified_by" json:"certified_by"`
	Runtimes    []string           `yaml:"runtimes" json:"runtimes"`
	Services    []ServiceReference `yaml:"services" json:"services"`
	Links       *ArchitectureLinks `yaml:"links,omitempty" json:"links,omitempty"`
}

// ArchitectureSummary represents an architecture for list API responses
type ArchitectureSummary struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	CertifiedBy string   `json:"certified_by"`
	Services    []string `json:"services"`
}

// ArchitectureLinks contains links related to an architecture
type ArchitectureLinks struct {
	Demo          string `yaml:"demo,omitempty" json:"demo,omitempty"`
	Code          string `yaml:"code,omitempty" json:"code,omitempty"`
	Documentation string `yaml:"documentation,omitempty" json:"documentation,omitempty"`
}

// ServiceReference represents a reference to a service in an architecture
type ServiceReference struct {
	ID       string `yaml:"id" json:"id"`
	Version  string `yaml:"version,omitempty" json:"version,omitempty"`
	Optional bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

// DependencyReference represents a reference to a dependency service
type DependencyReference struct {
	ID      string `yaml:"id" json:"id"`
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
}

// Service represents a deployable AI service
type Service struct {
	ID             string                `yaml:"id" json:"id"`
	Name           string                `yaml:"name" json:"name"`
	Description    string                `yaml:"description" json:"description"`
	Type           string                `yaml:"type" json:"type"` // "service"
	CertifiedBy    string                `yaml:"certified_by" json:"certified_by"`
	DependencyOnly bool                  `yaml:"dependency_only,omitempty" json:"dependency_only,omitempty"`
	Architectures  []string              `yaml:"architectures" json:"architectures"`
	Dependencies   []DependencyReference `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

// ServiceSummary represents a service for list API responses
type ServiceSummary struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	CertifiedBy   string   `json:"certified_by"`
	Architectures []string `json:"architectures"`
}

// RuntimeMetadata contains runtime-specific metadata
type RuntimeMetadata struct {
	Name    string `yaml:"name" json:"name"`
	Version string `yaml:"version" json:"version"`
}

// Made with Bob
