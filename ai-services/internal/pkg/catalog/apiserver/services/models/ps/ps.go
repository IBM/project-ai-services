package ps

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/worker"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
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

	pods, err := catalogutils.LoadPods(ctx, rt, row.ID.String())
	if err != nil {
		return nil, fmt.Errorf("failed to load pods for model %s: %w", row.ID, err)
	}

	resp.Pods = pods

	return resp, nil
}
