package types

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// DeploymentPlan represents the complete deployment plan for an application.
type DeploymentPlan struct {
	ApplicationID   uuid.UUID                 // Generated application ID
	ApplicationName string                    // Application name
	CatalogID       string                    // Architecture or service catalog ID
	IsArchitecture  bool                      // true for architecture, false for standalone service
	Components      map[string]*ComponentPlan // Key: component hash, Value: component plan
	Services        map[string]*ServicePlan   // Key: service ID, Value: service plan
	SpyreCardPool   *SpyreCardPool            // Allocated Spyre card pool (set after allocation)
}

// ComponentPlan represents a single component deployment.
type ComponentPlan struct {
	Hash           string         // Unique hash identifying this component configuration
	ComponentType  string         // e.g., "vector_db", "llm", "embedding"
	ProviderID     string         // e.g., "opensearch", "vllm"
	CatalogPath    string         // Dynamic catalog path (e.g., "components/llm/vllm-cpu/podman")
	DatabaseID     uuid.UUID      // Database UUID for this component record (set after DB insertion)
	Params         map[string]any // Component parameters
	UsedByServices []string       // List of service IDs that use this component
	Values         map[string]any // Structured values from LoadComponentValues
	Endpoints      map[string]any // Extracted endpoints after deployment (populated by deployer)
}

// ServicePlan represents a single service deployment.
type ServicePlan struct {
	CatalogID     string         // Service catalog ID (e.g., "chat", "digitize")
	CatalogPath   string         // Dynamic catalog path (e.g., "services/chat/podman")
	DatabaseID    uuid.UUID      // Database UUID for this service record (set after DB insertion)
	Version       string         // Service version
	ComponentRefs []string       // List of component hashes this service uses
	Values        map[string]any // Structured values from LoadServiceValues + component values
}

// SpyreCardPool manages allocation of PCI addresses to components.
type SpyreCardPool struct {
	Addresses []string
	mutex     sync.Mutex
}

// Allocate takes n addresses from the pool and returns them.
func (p *SpyreCardPool) Allocate(n int) ([]string, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if len(p.Addresses) < n {
		return nil, ErrInsufficientSpyreCards{Need: n, Have: len(p.Addresses)}
	}

	allocated := make([]string, n)
	copy(allocated, p.Addresses[:n])
	p.Addresses = p.Addresses[n:]

	return allocated, nil
}

// ErrInsufficientSpyreCards is returned when there are not enough Spyre cards available.
type ErrInsufficientSpyreCards struct {
	Need int
	Have int
}

func (e ErrInsufficientSpyreCards) Error() string {
	return fmt.Sprintf("insufficient Spyre cards in pool: need %d, have %d", e.Need, e.Have)
}

// Made with Bob
