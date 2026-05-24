package types

import "github.com/google/uuid"

// DeploymentPlan represents the complete deployment plan for an application.
type DeploymentPlan struct {
	ApplicationID   uuid.UUID                 // Generated application ID
	ApplicationName string                    // Application name
	CatalogID       string                    // Architecture or service catalog ID
	IsArchitecture  bool                      // true for architecture, false for standalone service
	Components      map[string]*ComponentPlan // Key: component hash, Value: component plan
	Services        map[string]*ServicePlan   // Key: service ID, Value: service plan
}

// ComponentPlan represents a single component deployment.
type ComponentPlan struct {
	Hash           string            // Unique hash identifying this component configuration
	ComponentType  string            // e.g., "vector_db", "llm", "embedding"
	ProviderID     string            // e.g., "opensearch", "vllm"
	CatalogPath    string            // Dynamic catalog path (e.g., "components/llm/vllm-cpu/podman")
	DatabaseID     uuid.UUID         // Database UUID for this component record (set after DB insertion)
	Params         map[string]any    // Component parameters
	ArgParams      map[string]string // Flattened params for template rendering
	UsedByServices []string          // List of service IDs that use this component
	Values         map[string]any    // Structured values from LoadComponentValues
	Endpoints      map[string]any    // Extracted endpoints after deployment (populated by deployer)
}

// ServicePlan represents a single service deployment.
type ServicePlan struct {
	CatalogID     string            // Service catalog ID (e.g., "chat", "digitize")
	CatalogPath   string            // Dynamic catalog path (e.g., "services/chat/podman")
	DatabaseID    uuid.UUID         // Database UUID for this service record (set after DB insertion)
	Version       string            // Service version
	ComponentRefs []string          // List of component hashes this service uses
	ArgParams     map[string]string // All params for this service (including component params)
	Values        map[string]any    // Structured values from LoadServiceValues + component values
}

// Made with Bob
