package openshift

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	openshiftutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/utils/openshift"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// OpenShiftDeleter removes a local model from an OpenShift runtime, local or remote over the
// worker stream.
type OpenShiftDeleter struct {
	componentRepo dbrepo.ComponentRepository
}

// NewOpenShiftDeleter creates a new OpenShiftDeleter.
func NewOpenShiftDeleter(componentRepo dbrepo.ComponentRepository) *OpenShiftDeleter {
	return &OpenShiftDeleter{
		componentRepo: componentRepo,
	}
}

// Delete uninstalls the model's Helm release from the shared models namespace.
// The models namespace is shared across all models and is preserved. The
// component row is deleted last, so it is kept, and the delete can be retried, if anything before it failed.
func (d *OpenShiftDeleter) Delete(ctx context.Context, rt runtime.Runtime, componentID uuid.UUID) error {
	comp, err := d.componentRepo.GetByID(ctx, componentID)
	if err != nil {
		return fmt.Errorf("failed to get component %s from DB: %w", componentID, err)
	}

	namespace := catalogutils.ModelsNamespace()
	helm := openshiftutils.NewHelmManager(rt, namespace)
	release := catalogutils.ModelHelmReleaseName(componentID, comp.Type)

	logger.InfofCtx(ctx, "Uninstalling '%s' model release in namespace '%s'.", release, namespace)

	if err := helm.Uninstall(ctx, release); err != nil {
		return fmt.Errorf("failed to uninstall model release '%s': %w", release, err)
	}

	if err := d.componentRepo.Delete(ctx, componentID); err != nil {
		return fmt.Errorf("failed to delete component %s from DB: %w", componentID, err)
	}

	return nil
}
