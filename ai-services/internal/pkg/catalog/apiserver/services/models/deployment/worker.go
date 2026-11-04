package deployment

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// workerResolver resolves the worker selected by a request and the runtime used to reach it.
type workerResolver struct {
	// repo is optional; without it only the control-plane worker can be targeted.
	repo     dbrepo.WorkerRepository
	registry stream.WorkerRegistry
	// runtimeFn overrides runtime(); used by tests.
	runtimeFn func(workerName string) (runtime.Runtime, error)
}

// resolve returns the worker for selector. An empty selector targets the control-plane worker.
func (r *workerResolver) resolve(ctx context.Context, selector string) (types.WorkerTarget, error) {
	explicit := selector != ""
	name := selector
	if !explicit {
		name = workerconstants.LocalWorkerName
	}

	if r.repo == nil {
		if explicit {
			return types.WorkerTarget{}, validators.NotFound("worker %q not found", selector)
		}

		return types.WorkerTarget{Name: name}, nil
	}

	worker, err := r.repo.GetByName(ctx, name)
	if err != nil {
		return types.WorkerTarget{}, fmt.Errorf("failed to look up worker %q: %w", name, err)
	}

	if worker == nil {
		if explicit {
			return types.WorkerTarget{}, validators.NotFound("worker %q not found", selector)
		}

		return types.WorkerTarget{Name: name}, nil
	}

	if explicit && worker.Status != dbmodels.WorkerStatusReady {
		return types.WorkerTarget{}, validators.BadRequest("worker %q is not ready", selector)
	}

	if r.runtimeType(worker) == string(dbmodels.WorkerRuntimeTypeOpenShift) {
		return types.WorkerTarget{}, validators.BadRequest("models are unsupported on openshift runtime")
	}

	return types.WorkerTarget{ID: &worker.ID, Name: worker.Name}, nil
}

// runtimeType returns the worker's runtime type, falling back to the live registry when the
// stored value has not been reported yet.
func (r *workerResolver) runtimeType(worker *dbmodels.Worker) string {
	rt := string(worker.RuntimeType)
	if rt != "" && rt != string(dbmodels.WorkerRuntimeTypeUnknown) {
		return rt
	}

	if r.registry != nil {
		if registered, ok := r.registry.WorkerRuntimeType(worker.Name); ok {
			return registered
		}
	}

	return rt
}

// runtime returns the runtime used to deploy to the named worker.
// Connected workers (local or remote) are reached over the gRPC CommandStream. When the local
// worker daemon is not connected, it falls back to a directly created local runtime; Spyre card
// discovery is unavailable in that case, but providers that need no cards (e.g. vllm-cpu) work.
func (r *workerResolver) runtime(workerName string) (runtime.Runtime, error) {
	if r.runtimeFn != nil {
		return r.runtimeFn(workerName)
	}

	if r.registry != nil {
		if workerRT, ok := r.registry.WorkerRuntimeType(workerName); ok {
			return runtime.NewRuntimeFactory(runtimeTypes.RuntimeType(workerRT)).CreateRemote(workerName, r.registry, "")
		}

		if workerName != workerconstants.LocalWorkerName {
			return nil, fmt.Errorf("worker is not connected")
		}
	}

	if vars.RuntimeFactory == nil {
		return nil, fmt.Errorf("local runtime factory not configured")
	}

	rt, err := vars.RuntimeFactory.Create("")
	if err != nil {
		return nil, fmt.Errorf("failed to create local runtime: %w", err)
	}

	return rt, nil
}
