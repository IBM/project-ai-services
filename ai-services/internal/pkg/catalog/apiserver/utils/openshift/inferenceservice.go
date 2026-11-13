package openshift

import (
	"context"
	"fmt"
	"io/fs"

	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// PredictorPort is the port of the Service KServe creates in RawDeployment mode.
const PredictorPort = "8080"

// PredictorAddress returns the "host:port" of the predictor Service of the InferenceService
// isvcName in namespace. KServe (RawDeployment) creates a Service named "<isvcName>-predictor", so
// the address is known before the predictor is up.
func PredictorAddress(isvcName, namespace string) string {
	return fmt.Sprintf("%s-predictor.%s.svc.cluster.local:%s", isvcName, namespace, PredictorPort)
}

// WaitForInferenceServiceReady waits until the InferenceService isvcName is Ready, but only when
// the chart at catalogPath deploys one. Helm marks an InferenceService "Current" as soon as the
// CRD is accepted, while KServe only sets Ready=True once the predictor pod is fully up, so the
// status has to be polled directly. rt must be scoped to the namespace the chart was installed in.
func WaitForInferenceServiceReady(ctx context.Context, rt runtime.Runtime, fsys fs.FS, catalogPath, isvcName string) error {
	if !catalogutils.ChartHasInferenceService(fsys, catalogPath) {
		return nil
	}

	logger.InfofCtx(ctx, "Waiting for InferenceService '%s' to become ready\n", isvcName)

	waitCtx, cancel := context.WithTimeout(ctx, constants.PredictorWaitTimeout)
	defer cancel()

	if err := rt.WaitForInferenceServiceReady(waitCtx, isvcName); err != nil {
		return fmt.Errorf("InferenceService %q is not ready yet: %w", isvcName, err)
	}

	return nil
}
