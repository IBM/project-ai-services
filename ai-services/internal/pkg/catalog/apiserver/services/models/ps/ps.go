package ps

import (
	"context"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/worker"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimecommon "github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// StatusGetter retrieves the process and pod status of a model.
type StatusGetter struct {
	workers worker.Resolver
}

// New creates a new StatusGetter.
func New(workers worker.Resolver) *StatusGetter {
	return &StatusGetter{
		workers: workers,
	}
}

// GetPS returns the process and pod status for a model row.
func (g *StatusGetter) GetPS(ctx context.Context, row *dbrepo.ModelRow) (*types.ModelPSResponse, error) {
	resp := &types.ModelPSResponse{
		ID:             row.ID.String(),
		Name:           row.Name,
		DeploymentType: string(row.DeploymentType),
		Status:         row.Status,
		Message:        row.Message,
		ComponentType:  row.ComponentType,
		ProviderID:     row.Provider,
		Model:          row.Model,
		Pods:           []types.Pod{},
	}

	if row.DeploymentType == catalogconstants.DeploymentTypeRemote {
		return resp, nil
	}

	workerName := worker.DefaultName(row.WorkerName)
	resp.WorkerName = workerName
	resp.RuntimeType = row.WorkerRuntimeType

	rt, err := g.workers.NewRuntime(ctx, workerName, catalogutils.ModelsNamespace())
	if err != nil {
		return nil, validators.Unprocessable("cannot reach worker %q: %v", workerName, err)
	}

	if runtimeTypes.RuntimeType(row.WorkerRuntimeType) == runtimeTypes.RuntimeTypeOpenShift {
		resp.Namespace = catalogutils.ModelsNamespace()
	}

	pods, err := loadComponentPods(ctx, rt, row.ID.String())
	if err != nil {
		logger.ErrorfCtx(ctx, "failed to load pods for model %s: %v", row.ID, err)

		return nil, fmt.Errorf("failed to load pods for model %s: %w", row.ID, err)
	}

	resp.Pods = pods

	return resp, nil
}

func loadComponentPods(ctx context.Context, rt runtime.Runtime, componentID string) ([]types.Pod, error) {
	filteredPods, err := runtimecommon.FetchFilteredPods(ctx, rt, componentID)
	if err != nil {
		return nil, err
	}
	if len(filteredPods) == 0 {
		return []types.Pod{}, nil
	}

	modelPods := make([]types.Pod, 0, len(filteredPods))
	for _, pod := range filteredPods {
		processedPod, err := runtimecommon.ProcessPod(ctx, rt, pod)
		if err != nil {
			return nil, fmt.Errorf("failed to process pod: %w", err)
		}
		if processedPod == nil {
			continue
		}

		containers := make([]types.PodContainer, 0, len(pod.Containers))
		for _, container := range processedPod.Containers {
			containers = append(containers, types.PodContainer{
				Name:    container.Name,
				Status:  types.Status(strings.ToLower(processedPod.Status)),
				Healthy: strings.ToLower(container.Health) == string(constants.Ready),
			})
		}

		modelPod := types.Pod{
			PodID:      processedPod.ID,
			PodName:    processedPod.Name,
			Status:     types.Status(strings.ToLower(processedPod.Status)),
			Healthy:    processedPod.Health == string(constants.Ready),
			Created:    pod.Created.Format(catalogconstants.RFC3339WithTimezone),
			Labels:     pod.Labels,
			Containers: containers,
		}
		modelPods = append(modelPods, modelPod)
	}

	return modelPods, nil
}
