package podman

import (
	"context"
	"fmt"
	texttemplate "text/template"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	deploymenttypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/utils/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// PodmanDeployer deploys model pods on a Podman runtime, local or remote over the worker stream.
type PodmanDeployer struct{}

// NewPodmanDeployer creates a new PodmanDeployer.
func NewPodmanDeployer() *PodmanDeployer {
	return &PodmanDeployer{}
}

// Deploy pulls the images, downloads the model, starts the pods and returns the model server's
// internal "host:port".
func (d *PodmanDeployer) Deploy(ctx context.Context, plan *types.LocalPlan) (string, error) {
	if err := preparePrerequisites(ctx, plan); err != nil {
		return "", err
	}

	spyrePool, err := allocateSpyreCards(ctx, plan)
	if err != nil {
		return "", err
	}

	baseDir, err := podmanutils.FetchBaseDir(ctx, plan.Runtime)
	if err != nil {
		return "", err
	}

	// Same code path as application components: it handles ordered PodTemplateExecutions, Spyre
	// env injection, the ai-services.io/template label used by deletion, and endpoint extraction.
	podEndpoints, err := podmanutils.DeployComponentPods(ctx, podmanutils.ComponentPodsParams{
		Runtime:       plan.Runtime,
		ComponentID:   plan.ComponentID,
		ComponentType: plan.Req.ComponentType,
		Values:        plan.Values,
		Metadata:      plan.Metadata,
		Templates:     plan.Templates,
		SpyrePool:     spyrePool,
		BaseDir:       baseDir,
	})
	if err != nil {
		return "", fmt.Errorf("failed to deploy component pods: %w", err)
	}

	return internalEndpoint(plan, podEndpoints)
}

// preparePrerequisites pulls the container images declared by the provider templates and
// downloads the model weights on the target worker, reusing the application deployment helpers.
func preparePrerequisites(ctx context.Context, plan *types.LocalPlan) error {
	// The tool image is required for the model download container and housekeeping tasks.
	imageSet := map[string]bool{vars.ToolImage: true}
	if err := plan.Catalog.CollectImagesFromTemplates(ctx, plan.Templates, plan.Values, imageSet); err != nil {
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

// allocateSpyreCards builds a Spyre card pool when the provider templates declare spyre-card
// annotations. It returns a nil pool for providers that need no cards.
func allocateSpyreCards(ctx context.Context, plan *types.LocalPlan) (*deploymenttypes.SpyreCardPool, error) {
	required, err := countSpyreCards(ctx, plan.Catalog, plan.Templates, plan.Values)
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
