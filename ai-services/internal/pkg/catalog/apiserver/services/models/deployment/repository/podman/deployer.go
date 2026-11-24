package podman

import (
	"context"
	"fmt"
	texttemplate "text/template"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	deploymenttypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/routes"
	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/utils/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// RouteRegistrar creates the private mTLS routes that connect the control plane to a model on a
// remote worker. Register returns the control-plane egress address of the model.
type RouteRegistrar interface {
	Register(ctx context.Context, rt runtime.Runtime, workerName string, componentID uuid.UUID, upstream string) (string, error)
}

// PodmanDeployer deploys model pods on a Podman runtime, local or remote over the worker stream.
type PodmanDeployer struct {
	routes RouteRegistrar
}

// NewPodmanDeployer creates a new PodmanDeployer. routes registers the private routes of models
// that run on a remote worker.
func NewPodmanDeployer(routes RouteRegistrar) *PodmanDeployer {
	return &PodmanDeployer{routes: routes}
}

// Check verifies that the worker has the Spyre cards the provider needs. It only discovers free
// cards; Start is what allocates them.
func (d *PodmanDeployer) Check(ctx context.Context, plan *types.LocalPlan) error {
	tmpls, err := loadTemplates(plan)
	if err != nil {
		return err
	}

	_, err = allocateSpyreCards(ctx, plan, tmpls)

	return err
}

// Prepare pulls the container images declared by the provider templates and downloads the model
// weights on the target worker, reusing the application deployment helpers.
func (d *PodmanDeployer) Prepare(ctx context.Context, plan *types.LocalPlan) error {
	tmpls, err := loadTemplates(plan)
	if err != nil {
		return err
	}

	// The tool image is required for the model download container and housekeeping tasks.
	imageSet := map[string]bool{vars.ToolImage: true}
	if err := plan.Catalog.CollectImagesFromTemplates(ctx, tmpls, plan.Values, imageSet); err != nil {
		return fmt.Errorf("failed to collect images for %s/%s: %w", plan.Req.ComponentType, plan.Req.ProviderID, err)
	}

	images := make([]string, 0, len(imageSet))
	for img := range imageSet {
		images = append(images, img)
	}

	logger.InfofCtx(ctx, "Pulling images for model component %s", plan.ComponentID)
	if err := podmanutils.PullImages(ctx, plan.Runtime, images); err != nil {
		return fmt.Errorf("image pull failed: %w", err)
	}

	logger.InfofCtx(ctx, "Downloading model %q for component %s", plan.Req.Model, plan.ComponentID)
	if err := podmanutils.DownloadModels(ctx, plan.Runtime, []string{plan.Req.Model}); err != nil {
		return fmt.Errorf("model download failed: %w", err)
	}

	return nil
}

// Start allocates Spyre cards when the provider needs them, starts the pods and returns the
// model server's endpoints.
//
// A model on the local worker is reached at the pod's own address. For a remote worker the pod is
// not reachable from the control plane, so the worker mTLS ingress and control-plane egress routes
// are created and the control-plane egress is returned as Egress.
func (d *PodmanDeployer) Start(ctx context.Context, plan *types.LocalPlan) (types.ModelEndpoints, error) {
	metadata, err := plan.Catalog.LoadComponentRuntimeMetadata(plan.Req.ComponentType, plan.Req.ProviderID)
	if err != nil {
		return types.ModelEndpoints{}, fmt.Errorf("failed to load component metadata: %w", err)
	}

	tmpls, err := loadTemplates(plan)
	if err != nil {
		return types.ModelEndpoints{}, err
	}

	spyrePool, err := allocateSpyreCards(ctx, plan, tmpls)
	if err != nil {
		return types.ModelEndpoints{}, err
	}

	baseDir, err := podmanutils.FetchBaseDir(ctx, plan.Runtime)
	if err != nil {
		return types.ModelEndpoints{}, err
	}

	// Same code path as application components: it handles ordered PodTemplateExecutions, Spyre
	// env injection, the ai-services.io/template label used by deletion, and endpoint extraction.
	podEndpoints, err := podmanutils.DeployComponentPods(ctx, podmanutils.ComponentPodsParams{
		Runtime:       plan.Runtime,
		ComponentID:   plan.ComponentID,
		ComponentType: plan.Req.ComponentType,
		Values:        plan.Values,
		Metadata:      metadata,
		Templates:     tmpls,
		SpyrePool:     spyrePool,
		BaseDir:       baseDir,
	})
	if err != nil {
		return types.ModelEndpoints{}, fmt.Errorf("failed to deploy component pods: %w", err)
	}

	internal, err := internalEndpoint(plan, podEndpoints)
	if err != nil {
		return types.ModelEndpoints{}, err
	}

	return d.routeEndpoints(ctx, plan, internal)
}

// routeEndpoints returns the endpoints of a started model. On the local worker the control plane
// reaches the pod directly. On a remote worker it cannot, so the private routes are created and
// the control-plane egress is the address to use.
func (d *PodmanDeployer) routeEndpoints(ctx context.Context, plan *types.LocalPlan, internal string) (types.ModelEndpoints, error) {
	if routes.IsLocal(plan.WorkerName) {
		return types.ModelEndpoints{Internal: internal, Egress: internal}, nil
	}

	egress, err := d.routes.Register(ctx, plan.Runtime, plan.WorkerName, plan.ComponentID, internal)
	if err != nil {
		return types.ModelEndpoints{}, fmt.Errorf("failed to route model %s on worker %q: %w", plan.ComponentID, plan.WorkerName, err)
	}

	return types.ModelEndpoints{Internal: internal, Egress: egress}, nil
}

// loadTemplates loads the provider's pod templates from the plan's runtime-scoped catalog.
func loadTemplates(plan *types.LocalPlan) (map[string]*texttemplate.Template, error) {
	tmpls, err := plan.Catalog.LoadComponentTemplates(plan.Req.ComponentType, plan.Req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("failed to load component templates: %w", err)
	}

	return tmpls, nil
}

// allocateSpyreCards builds a Spyre card pool when the provider templates declare spyre-card
// annotations. It returns a nil pool for providers that need no cards.
func allocateSpyreCards(
	ctx context.Context,
	plan *types.LocalPlan,
	tmpls map[string]*texttemplate.Template,
) (*deploymenttypes.SpyreCardPool, error) {
	required, err := countSpyreCards(ctx, plan.Catalog, tmpls, plan.Values)
	if err != nil {
		return nil, err
	}

	if required == 0 {
		return nil, nil //nolint:nilnil // a nil pool means "no cards required".
	}

	logger.InfofCtx(ctx, "Model requires %d Spyre card(s)", required)

	pool, err := podmanutils.NewSpyreCardPool(ctx, plan.Runtime, required)
	if err != nil {
		return nil, fmt.Errorf("spyre card allocation failed: %w", err)
	}

	return pool, nil
}

func countSpyreCards(
	ctx context.Context,
	provider *catalog.CatalogProvider,
	tmpls map[string]*texttemplate.Template,
	values map[string]any,
) (int, error) {
	required, err := provider.CollectSpyreCardsFromTemplates(ctx, tmpls, values)
	if err != nil {
		return 0, fmt.Errorf("failed to count required Spyre cards: %w", err)
	}

	return required, nil
}

// internalEndpoint returns the model server's "host:port" as extracted from the container port
// declared by the provider's pod template. The port is deliberately not defaulted: it differs per
// provider (e.g. llm, embedding and reranker listen on different ports), so a template that does
// not declare one is an error rather than a guess that would register an unreachable route.
func internalEndpoint(plan *types.LocalPlan, podEndpoints map[string]any) (string, error) {
	componentType := plan.Req.ComponentType

	if ep, ok := podEndpoints[componentType].(map[string]any); ok {
		host, _ := ep["host"].(string)
		port, _ := ep["port"].(string)
		if host != "" && port != "" {
			return fmt.Sprintf("%s:%s", host, port), nil
		}
	}

	return "", fmt.Errorf("provider %s/%s did not expose a container port in its pod template", componentType, plan.Req.ProviderID)
}
