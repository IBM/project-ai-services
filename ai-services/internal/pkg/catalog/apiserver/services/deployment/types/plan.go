package types

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/google/uuid"
	modeltypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
)

// DeploymentPlan represents the complete deployment plan for an application.
type DeploymentPlan struct {
	ApplicationID   uuid.UUID                 // Generated application ID
	ApplicationName string                    // Application name
	CreatedBy       string                    // User creating the application; models it attaches to must be theirs
	Namespace       string                    // Namespace derived from ApplicationID
	CatalogID       string                    // Architecture or service catalog ID
	Version         string                    // Application version from request
	IsArchitecture  bool                      // true for architecture, false for standalone service
	Components      map[string]*ComponentPlan // Key: component hash, Value: component plan
	Services        map[string]*ServicePlan   // Key: service ID, Value: service plan
	// WorkerName identifies the target worker. Always set by PlanDeployment:
	// workerconstants.LocalWorkerName for local deployments, or the actual
	// worker name for remote ones.
	WorkerName string
	// RuntimeType is the resolved runtime for this deployment (e.g. "podman" or
	// "openshift"). For worker deployments it is the worker's registered runtime;
	// for local deployments it is the server's configured runtime. Set by
	// PlanDeployment and used as the single source of truth by the executor and
	// deployer, avoiding any implicit reads of vars.RuntimeFactory.
	RuntimeType string

	// valuesMu guards the values of the services while components deploy concurrently: every
	// component writes the address it is reached at into the services that use it.
	valuesMu sync.Mutex
}

// ModelComponents returns the components the model service deploys for this application, in a
// stable order (by hash), so models are downloaded and started in the same order on every run.
func (p *DeploymentPlan) ModelComponents() []*ComponentPlan {
	var out []*ComponentPlan

	for _, hash := range slices.Sorted(maps.Keys(p.Components)) {
		if comp := p.Components[hash]; comp.Model != nil {
			out = append(out, comp)
		}
	}

	return out
}

// NonModelComponents returns the components a runtime deployer deploys itself, that is all but the
// model components, which the model service handles.
func (p *DeploymentPlan) NonModelComponents() map[string]*ComponentPlan {
	out := make(map[string]*ComponentPlan, len(p.Components))

	for hash, comp := range p.Components {
		if !comp.IsModel() {
			out[hash] = comp
		}
	}

	return out
}

// ComponentIDs returns the database ID of every component of the plan by hash, which is what
// catalogutils.RunConcurrently needs to deploy them concurrently and track their status.
func (p *DeploymentPlan) ComponentIDs() map[string]uuid.UUID {
	ids := make(map[string]uuid.UUID, len(p.Components))
	for hash, comp := range p.Components {
		ids[hash] = comp.DatabaseID
	}

	return ids
}

// ComponentStatusFunc returns a catalogutils.StatusFunc that moves a component of the plan to
// status. It leaves the models alone: the model service reports their status itself, and an
// existing instance, which the application only uses, is never touched.
func (p *DeploymentPlan) ComponentStatusFunc(repo repository.ComponentRepository, status models.ComponentStatus) catalogutils.StatusFunc {
	modelIDs := make(map[uuid.UUID]bool)

	for _, comp := range p.Components {
		if comp.IsModel() {
			modelIDs[comp.DatabaseID] = true
		}
	}

	return func(ctx context.Context, id uuid.UUID, msg string) error {
		if modelIDs[id] {
			return nil
		}

		return catalogutils.UpdateComponentStatus(ctx, repo, id, status, msg)
	}
}

// ComponentPlan represents a single component deployment.
type ComponentPlan struct {
	Hash           string         // Unique hash identifying this component configuration
	ComponentType  string         // e.g., "vector_db", "llm", "embedding"
	ProviderID     string         // e.g., "opensearch", "vllm"
	CatalogPath    string         // Dynamic catalog path (e.g., "components/llm/vllm-cpu/podman")
	DatabaseID     uuid.UUID      // Database UUID for this component record (set after DB insertion)
	Version        string         // Component version
	Params         map[string]any // Component parameters
	UsedByServices []string       // List of service IDs that use this component
	Values         map[string]any // Structured values from LoadComponentValues
	Endpoints      map[string]any // Extracted endpoints after deployment (populated by deployer)

	// Model is the model service's plan for a model component (llm, embedding, reranker, ...) that
	// is deployed by this application. The model service owns its records, download, deployment
	// and status, so the deployer does not treat the component like any other. Nil for every other
	// component, and for a model that already runs (see PreDeployed).
	Model *modeltypes.LocalPlan
	// PreDeployed marks a model component bound to an existing running instance: nothing is
	// inserted or deployed, and DatabaseID is the instance's ID.
	PreDeployed bool
	// Remote marks a PreDeployed model that is a remote model connector: DatabaseID is the
	// connector's ID, and services depend on it as a connector, not as a component.
	Remote bool
}

// IsModel reports whether the component is a model handled by the model service, either one to
// deploy (Model) or an existing instance (PreDeployed).
func (c *ComponentPlan) IsModel() bool {
	return c.Model != nil || c.PreDeployed
}

// ServicePlan represents a single service deployment.
type ServicePlan struct {
	CatalogID        string            // Service catalog ID (e.g., "chat", "digitize")
	CatalogPath      string            // Dynamic catalog path (e.g., "services/chat/podman")
	DatabaseID       uuid.UUID         // Database UUID for this service record (set after DB insertion)
	Version          string            // Service version
	ComponentRefs    []string          // List of component hashes this service uses
	Values           map[string]any    // Structured values from LoadServiceValues + component values
	Routes           map[string]string // Routes extracted during deployment: podName -> routes annotation
	InternalEndpoint string            // Internal pod-to-pod endpoint URL (http://podname:port), populated by deployer
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
