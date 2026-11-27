package openshift

import (
	"context"
	"fmt"
	"io/fs"
	"time"

	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// PredictorPort is the port the model server listens on. It is the containerPort of the serving runtime container.
const PredictorPort = "8000"

// PredictorAddress returns the "host:port" of the predictor Service of the InferenceService
// isvcName in namespace. KServe (RawDeployment) creates a Service named "<isvcName>-predictor", so
// the address is known before the predictor is up.
func PredictorAddress(isvcName, namespace string) string {
	return fmt.Sprintf("%s-predictor.%s.svc.cluster.local:%s", isvcName, namespace, PredictorPort)
}

// WaitForInferenceService waits until the InferenceService isvcName is Ready, but only when the chart
// at catalogPath deploys one. Helm marks an InferenceService "Current" as soon as the CRD is accepted,
// while KServe only sets Ready=True once the predictor is up, so the status has to be polled directly.
// A missing InferenceService is not ready, so it is polled for until it appears. rt must be scoped to
// the namespace the chart was installed in.
func WaitForInferenceService(ctx context.Context, rt runtime.Runtime, fsys fs.FS, catalogPath, isvcName string) error {
	if !catalogutils.ChartHasInferenceService(fsys, catalogPath) {
		return nil
	}

	logger.InfofCtx(ctx, "Waiting for InferenceService '%s' to become ready\n", isvcName)

	waitCtx, cancel := context.WithTimeout(ctx, constants.PredictorWaitTimeout)
	defer cancel()

	if err := pollInferenceServiceReady(waitCtx, rt, isvcName); err != nil {
		return fmt.Errorf("InferenceService %q is not ready yet: %w", isvcName, err)
	}

	return nil
}

// pollInferenceServiceReady polls the named KServe InferenceService until its Ready condition is
// True, or ctx is done.
func pollInferenceServiceReady(ctx context.Context, rt runtime.Runtime, isvcName string) error {
	for {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "serving.kserve.io",
			Version: "v1beta1",
			Kind:    "InferenceServiceList",
		})

		isvcs, err := rt.ListCRD(ctx, list, map[string][]string{"name": {isvcName}})
		if err != nil {
			return fmt.Errorf("failed to get InferenceService %q: %w", isvcName, err)
		}

		if inferenceServiceReady(isvcs, isvcName) {
			logger.InfofCtx(ctx, "InferenceService %q is ready\n", isvcName)

			return nil
		}

		logger.InfofCtx(ctx, "InferenceService %q not ready yet, retrying in %s\n", isvcName, constants.IsvcPollInterval)

		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for InferenceService %q to become ready: %w", isvcName, ctx.Err())
		case <-time.After(constants.IsvcPollInterval):
		}
	}
}

// inferenceServiceReady reports whether the InferenceService isvcName is present in isvcs and Ready.
func inferenceServiceReady(isvcs []runtimeTypes.CRDResource, isvcName string) bool {
	for _, isvc := range isvcs {
		if isvc.Name == isvcName {
			return isvc.Ready
		}
	}

	return false
}
