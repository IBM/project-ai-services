package openshift

import (
	"context"
	"errors"
	"fmt"

	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	helmutil "github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// UninstallServingRuntimes uninstalls the prerequisite ServingRuntime releases found in the
// namespace rt is scoped to. It collects and returns every error using errors.Join rather than stopping at the first.
// It is idempotent: if the namespace or CRD does not exist, it returns nil.
func UninstallServingRuntimes(ctx context.Context, rt runtime.Runtime, helm helmutil.HelmManager) error {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "serving.kserve.io",
		Version: "v1alpha1",
		Kind:    "ServingRuntimeList",
	})

	response, err := rt.ListCRD(ctx, list, map[string][]string{
		"label": {constants.PrerequisiteLabelKey},
	})
	if err != nil {
		if k8serrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			logger.DebugfCtx(ctx, "Skipping serving runtime uninstall: %v\n", err)

			return nil
		}

		errMsg := fmt.Sprintf("failed to list serving runtimes: %s", err)
		logger.ErrorlnCtx(ctx, errMsg)

		return fmt.Errorf("failed to list serving runtimes: %w", err)
	}

	var errs []error

	for _, item := range response {
		release := item.Labels[constants.PrerequisiteLabelKey]
		if release == "" {
			release = item.Name
		}

		logger.InfofCtx(ctx, "Uninstalling '%s' serving runtime release.", release)

		if err := helm.Uninstall(ctx, release); err != nil {
			errs = append(errs, fmt.Errorf("serving runtime '%s': helm uninstall failed: %w", release, err))
		}
	}

	return errors.Join(errs...)
}

// CleanupModelsNamespace uninstalls all serving runtimes from the shared models namespace and deletes the namespace.
func CleanupModelsNamespace(ctx context.Context, rt runtime.Runtime) error {
	modelsNamespace := catalogconstants.CatalogModelsNamespace
	modelsHelm := NewHelmManager(rt, modelsNamespace)

	if err := UninstallServingRuntimes(ctx, rt, modelsHelm); err != nil {
		return fmt.Errorf("failed to uninstall serving runtimes in %s: %w", modelsNamespace, err)
	}

	if err := rt.DeleteNamespace(ctx, modelsNamespace); err != nil {
		return fmt.Errorf("failed to delete '%s' namespace: %w", modelsNamespace, err)
	}

	return nil
}
