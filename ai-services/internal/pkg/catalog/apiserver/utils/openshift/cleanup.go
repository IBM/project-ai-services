package openshift

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	helmutil "github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// UninstallServingRuntimes uninstalls the prerequisite ServingRuntime releases found in the
// namespace rt is scoped to. It collects and returns every error rather than stopping at the first.
func UninstallServingRuntimes(ctx context.Context, rt runtime.Runtime, helm helmutil.HelmManager) []string {
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
		errMsg := fmt.Sprintf("failed to list serving runtimes: %s", err)
		logger.ErrorlnCtx(ctx, errMsg)

		return []string{errMsg}
	}

	var errorMessages []string

	for _, item := range response {
		release := item.Labels[constants.PrerequisiteLabelKey]
		if release == "" {
			release = item.Name
		}

		logger.InfofCtx(ctx, "Uninstalling '%s' serving runtime release.", release)

		if err := helm.Uninstall(ctx, release); err != nil {
			errorMessages = append(errorMessages, fmt.Sprintf("serving runtime '%s': helm uninstall failed: %s", release, err))
		}
	}

	return errorMessages
}
