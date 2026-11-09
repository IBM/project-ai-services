package types

import (
	"context"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/litellm"
	catalogtypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// Deployer deploys a planned local model on one kind of runtime in three steps: Check, Prepare
// and Start, so a caller can reject a request up front and run the slow download step ahead of,
// or apart from, starting the pods. Supporting another runtime means adding an implementation
// under repository/ and registering it with the planner; the plan and execute phases do not
// change.
//
// TODO(mTLS): for a non-local worker, Start must create the worker mTLS ingress/egress Caddy
// routes (worker :8443 -> pod, control-plane private egress -> worker) and return the
// control-plane private egress as Egress, keeping the pod's own address as Internal. Those
// routes must also be removed on undeploy.
type Deployer interface {
	// Check verifies, without side effects, that the worker can host the model right now (e.g.
	// enough free accelerator cards), so an unsatisfiable request is rejected during planning
	// before anything is written or downloaded. It is a point-in-time check: Start still has to
	// allocate for real and can fail if the resources were taken in between.
	Check(ctx context.Context, plan *LocalPlan) error
	// Prepare makes the worker ready to start the model: it pulls the container images and
	// downloads the model weights. It starts nothing and is safe to call again.
	Prepare(ctx context.Context, plan *LocalPlan) error
	// Start allocates any accelerator cards, starts the pods and returns where the model server
	// can be reached. It expects Prepare to have succeeded.
	Start(ctx context.Context, plan *LocalPlan) (ModelEndpoints, error)
}

// ModelEndpoints are the "host:port" addresses of a deployed model server. They are two values
// because they are different facts: where the pod is, and how the control plane gets to it. They
// are equal only when the pod runs on the local worker.
type ModelEndpoints struct {
	// Internal is the pod's own address on its worker. It is persisted as the component's
	// "internal" endpoint.
	Internal string
	// Egress is the address the control plane uses to reach the model, so it is what LiteLLM's
	// api_base and tokenize target are built from. For a remote worker it is the control-plane
	// private egress, not the pod.
	Egress string
}

// LocalPlan is a fully resolved local-model deployment.
type LocalPlan struct {
	ComponentID uuid.UUID
	Req         apimodels.CreateModelRequest
	WorkerName  string
	WorkerID    *uuid.UUID
	Runtime     runtime.Runtime
	Deployer    Deployer
	// RootMetadata is the provider's metadata section.
	RootMetadata *catalogtypes.Component
	// Route is the gateway route of the model. It has no APIBase or TokenizeURL yet: those are
	// derived from the endpoint the Deployer returns, so the executor completes it after Start.
	Route litellm.Route
	// Metadata is what gets persisted on the component row: the model, the profile and the
	// non-sensitive params nested under "params".
	Metadata map[string]any
	// Catalog is scoped to the runtime the pods are deployed on. The Deployer loads the
	// provider's templates and runtime metadata from it.
	Catalog *catalog.CatalogProvider
	// Values are the rendered template values: provider defaults overridden by params, plus the
	// requested model as .Values.model.
	Values map[string]any
}

// RemotePlan is a fully validated remote-model registration.
type RemotePlan struct {
	ConnectorID uuid.UUID
	Req         apimodels.CreateModelRequest
	Route       litellm.Route
	// Metadata is what gets persisted: the non-sensitive params only.
	Metadata map[string]any
}
