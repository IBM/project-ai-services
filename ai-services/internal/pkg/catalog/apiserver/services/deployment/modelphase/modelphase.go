// Package modelphase runs the phases of the model components of a deployment plan through the
// model service. It is shared by the runtime deployers (Podman, OpenShift) so that every runtime
// downloads, deploys and fails an application's models the same way. It only handles models: the
// other components of the plan are deployed by the runtime's deployer, which hands each model to
// DeployModel from the goroutine that deploys the components concurrently.
//
// A model component is not deployed like the other components: a model deployed by an
// application is downloaded, started, exposed and reported exactly like one created through
// POST /api/v1/models. Models are downloaded one at a time, as downloads are bandwidth bound, and
// started concurrently with the other components.
package modelphase

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	modeltypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// Runner runs the download, deploy and fail phases of a plan's model components.
type Runner struct {
	models types.ModelManager
}

// New returns a Runner that delegates every phase to models, which may be nil when no plan it runs
// has model components.
func New(models types.ModelManager) *Runner {
	return &Runner{models: models}
}

// Download is the download phase: every model of the plan is downloaded, while its component is
// Downloading. It does not report a failure: the caller fails the plan's models with Fail.
func (r *Runner) Download(ctx context.Context, plan *types.DeploymentPlan) error {
	models := plan.ModelComponents()
	if len(models) == 0 {
		return nil
	}

	logger.InfofCtx(ctx, "Downloading %d model(s) for application '%s'\n", len(models), plan.ApplicationName)

	for _, comp := range models {
		if err := r.models.DownloadLocalModel(ctx, comp.Model); err != nil {
			return fmt.Errorf("failed to download model %s: %w", comp.Model.Req.Name, err)
		}
	}

	return nil
}

// DeployModel is the deploy phase of one model component, which ends with its services reaching it:
//   - A model to deploy is started and becomes Running. A failure moves it to Error. A model that
//     already runs is not started.
//   - The values the model service returns (see ModelManager.GatewayValues) are merged into the
//     services that use the model, so they reach it through the gateway with a key of the
//     application, never at the model server. A failure to get them fails the deployment, but not
//     the model, which outlives the application.
//
// Deployers call it for each model of the plan from the goroutine that deploys the components
// concurrently, so models start while the rest deploys.
func (r *Runner) DeployModel(ctx context.Context, plan *types.DeploymentPlan, comp *types.ComponentPlan) error {
	if comp.Model != nil {
		if err := r.start(ctx, comp); err != nil {
			return err
		}
	}

	model, _ := comp.Params[constants.ParamKeyModel].(string)

	values, err := r.models.GatewayValues(ctx, modeltypes.ModelAccess{
		ComponentID:   comp.DatabaseID,
		Model:         model,
		ApplicationID: plan.ApplicationID,
		Runtime:       runtimeTypes.RuntimeType(plan.RuntimeType),
		WorkerName:    plan.WorkerName,
	})
	if err != nil {
		return fmt.Errorf("failed to give the services access to model %s: %w", comp.ComponentType, err)
	}

	for _, serviceID := range comp.UsedByServices {
		plan.MergeComponentValues(ctx, serviceID, comp.ComponentType, values)
	}

	return nil
}

// start starts the model of comp, or moves it to Error.
func (r *Runner) start(ctx context.Context, comp *types.ComponentPlan) error {
	if err := r.models.DeployLocalModel(ctx, comp.Model); err != nil {
		err = fmt.Errorf("failed to deploy model %s: %w", comp.Model.Req.Name, err)
		r.models.FailLocalModel(ctx, comp.Model, err)

		return err
	}

	return nil
}

// Fail moves every model of the plan to Error, with cause as the message.
func (r *Runner) Fail(ctx context.Context, plan *types.DeploymentPlan, cause error) {
	for _, comp := range plan.ModelComponents() {
		r.models.FailLocalModel(ctx, comp.Model, cause)
	}
}
