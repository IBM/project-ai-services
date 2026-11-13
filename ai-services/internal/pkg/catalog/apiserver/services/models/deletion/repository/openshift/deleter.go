package openshift

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	return &OpenShiftDeleter{componentRepo: componentRepo}
}

// Delete follows the application deletion: uninstall the serving-runtime releases, then delete the
// namespace. The namespace belongs to the model alone, so deleting it also removes the model's
// InferenceService, its volumes and its Helm release. The component row is deleted last, so it is
// kept, and the delete can be retried, if anything before it failed.
func (d *OpenShiftDeleter) Delete(ctx context.Context, rt runtime.Runtime, componentID uuid.UUID) error {
	namespace := catalogutils.ModelNamespace(componentID)
	helm := openshiftutils.NewHelmManager(rt, namespace)

	if errs := openshiftutils.UninstallServingRuntimes(ctx, rt, helm); len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}

	logger.InfofCtx(ctx, "Deleting '%s' namespace.", namespace)

	if err := rt.DeleteNamespace(ctx, namespace); err != nil {
		return fmt.Errorf("failed to delete '%s' namespace: %w", namespace, err)
	}

	if err := d.componentRepo.Delete(ctx, componentID); err != nil {
		return fmt.Errorf("failed to delete component %s from DB: %w", componentID, err)
	}

	return nil
}
