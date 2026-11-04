package deployment

import (
	"context"
	"fmt"
	"maps"
	"strings"
	texttemplate "text/template"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/repository/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	clitemplates "github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

const (
	// valuesKeyModel is the template value (.Values.model) that carries the requested model.
	valuesKeyModel = "model"

	// paramAPIBase is the litellm_params key of the model endpoint. It is the only provider param
	// the platform interprets itself (to derive the /tokenize target); every other param is
	// forwarded to LiteLLM untouched.
	paramAPIBase = "api_base"

	// watsonxTokenizeURL is the tokenize passthrough target for watsonx remote models.
	watsonxTokenizeURL = "http://localhost:4000/watsonx/ml/v1/text/tokenization"
)

// ModelPlanner plans model deployments by:
//  1. Validating the request against the catalog (component type, provider, params schema)
//  2. Resolving the target worker, its runtime and the runtime's deployer (local models)
//  3. Loading the provider's templates and building the template values / gateway route
//
// Planning has no side effects, so a bad request is rejected with a proper status code before any
// row is written or any background work starts.
type ModelPlanner struct {
	catalogProvider *catalog.CatalogProvider
	models          modelCatalog
	workers         *workerResolver
	// deployers maps a runtime type to the deployer that starts local model pods on it.
	deployers map[runtimeTypes.RuntimeType]types.Deployer
}

// NewModelPlanner creates a new model planner with the default per-runtime deployers.
func NewModelPlanner(provider *catalog.CatalogProvider) *ModelPlanner {
	return &ModelPlanner{
		catalogProvider: provider,
		models:          modelCatalog{provider: provider},
		workers:         &workerResolver{},
		deployers: map[runtimeTypes.RuntimeType]types.Deployer{
			runtimeTypes.RuntimeTypePodman: podman.NewPodmanDeployer(),
		},
	}
}

// WithWorkerRepo wires the worker repository into the planner. Without it only the control-plane
// worker can be targeted.
func (p *ModelPlanner) WithWorkerRepo(repo dbrepo.WorkerRepository) *ModelPlanner {
	p.workers.repo = repo

	return p
}

// WithWorkerRegistry wires the worker registry into the planner so it can resolve a runtime for
// workers connected over the gRPC CommandStream.
func (p *ModelPlanner) WithWorkerRegistry(reg stream.WorkerRegistry) *ModelPlanner {
	p.workers.registry = reg

	return p
}

// WithDeployers replaces the per-runtime deployers.
func (p *ModelPlanner) WithDeployers(deployers map[runtimeTypes.RuntimeType]types.Deployer) *ModelPlanner {
	p.deployers = deployers

	return p
}

// WithRuntimeResolver overrides how the runtime of a worker is resolved; used by tests.
func (p *ModelPlanner) WithRuntimeResolver(fn func(workerName string) (runtime.Runtime, error)) *ModelPlanner {
	p.workers.runtimeFn = fn

	return p
}

// ValidateComponentType rejects a component type that has no model providers in the catalog.
func (p *ModelPlanner) ValidateComponentType(componentType string) error {
	return p.models.validateType(componentType)
}

// PlanLocal validates a local-model request and resolves where and how it will be deployed.
func (p *ModelPlanner) PlanLocal(ctx context.Context, req apimodels.CreateModelRequest) (*types.LocalPlan, error) {
	target, err := p.workers.resolve(ctx, req.WorkerSelector)
	if err != nil {
		return nil, err
	}

	// Resolving the runtime up front means an unreachable worker fails the request (422)
	// instead of leaving a component row stuck in Deploying.
	rt, err := p.workers.runtime(target.Name)
	if err != nil {
		return nil, validators.Unprocessable("cannot reach worker %q: %v", target.Name, err)
	}

	deployer, ok := p.deployers[rt.Type()]
	if !ok {
		return nil, validators.Unprocessable("models are unsupported on %s runtime", rt.Type())
	}

	// Scope the catalog to the worker's actual runtime so templates, metadata, and values
	// are loaded from the correct runtime-specific asset path.
	provider, err := p.catalogProvider.WithRuntime(rt.Type().String())
	if err != nil {
		return nil, fmt.Errorf("failed to scope catalog provider: %w", err)
	}

	comp, sensitive, err := validateLocalParams(ctx, provider, req)
	if err != nil {
		return nil, err
	}

	metadata, tmpls, values, err := loadLocalAssets(provider, req)
	if err != nil {
		return nil, err
	}

	plan := &types.LocalPlan{
		ComponentID:       uuid.New(),
		Req:               req,
		Worker:            target,
		Runtime:           rt,
		Deployer:          deployer,
		LiteLLM:           *comp.LiteLLM,
		Catalog:           provider,
		Metadata:          metadata,
		ComponentMetadata: localComponentMetadata(req, sensitive),
		Templates:         tmpls,
		Values:            values,
	}

	// Reject what the worker cannot host (e.g. not enough free Spyre cards) now, instead of after
	// the row is written and the images and weights are downloaded.
	if err := deployer.Check(ctx, plan); err != nil {
		return nil, validators.Unprocessable("worker %q cannot host this model: %v", target.Name, err)
	}

	return plan, nil
}

// localComponentMetadata is what is persisted on the component row: the top-level model and
// profile, with the non-sensitive params nested under "params". Credentials only reach the pod
// through the template values.
func localComponentMetadata(req apimodels.CreateModelRequest, sensitive map[string]bool) map[string]any {
	md := map[string]any{
		constants.MetadataKeyModel:  req.Model,
		constants.MetadataKeyParams: catalogutils.StripSensitiveFields(req.Params, sensitive),
	}

	if req.Profile != "" {
		md[constants.MetadataKeyProfile] = req.Profile
	}

	return md
}

// loadLocalAssets loads the provider's runtime metadata and templates and builds the template values.
func loadLocalAssets(
	provider *catalog.CatalogProvider,
	req apimodels.CreateModelRequest,
) (*clitemplates.AppMetadata, map[string]*texttemplate.Template, map[string]any, error) {
	componentType := req.ComponentType

	metadata, err := provider.LoadComponentRuntimeMetadata(componentType, req.ProviderID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load component metadata: %w", err)
	}

	tmpls, err := provider.LoadComponentTemplates(componentType, req.ProviderID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to load component templates: %w", err)
	}

	// A provider without a values file is valid: fall back to request-only values.
	values, err := provider.LoadComponentValues(componentType, req.ProviderID, map[string]string{})
	if err != nil || values == nil {
		values = make(map[string]any)
	}

	maps.Copy(values, req.Params)

	// The templates read the model as .Values.model; it is a top-level request field, not a param.
	values[valuesKeyModel] = req.Model

	// TODO(profiles): validate req.Profile against the provider's profiles.yaml (404/400 when
	// unknown) and resolve it into concrete deployment settings (context length, resources, ...)
	// that get merged into values here.

	return metadata, tmpls, values, nil
}

// PlanRemote validates a remote-model request against the provider's schema and builds its route.
func (p *ModelPlanner) PlanRemote(ctx context.Context, req apimodels.CreateModelRequest) (*types.RemotePlan, error) {
	sensitive, err := validateRemoteParams(ctx, p.catalogProvider, req)
	if err != nil {
		return nil, err
	}

	mode, err := p.models.modeFor(req.ComponentType)
	if err != nil {
		return nil, err
	}

	connectorID := uuid.New()

	// The params were validated against the provider's schema.json, so they are forwarded to
	// LiteLLM as is: a field added to the schema reaches the gateway without a code change.
	r := types.Route{
		Tag:           connectorID.String(),
		Provider:      req.ProviderID,
		Mode:          mode,
		Model:         req.Model,
		UpstreamModel: fmt.Sprintf("%s/%s", req.ProviderID, req.Model),
		Params:        maps.Clone(req.Params),
	}
	// A tokenize passthrough is registered by default. A provider whose endpoint cannot be
	// reached at the resolved target will cause registration to fail.
	r.TokenizeURL = remoteTokenizeURL(req.ProviderID, req.Params)

	// Credentials go to the gateway only; the catalog DB keeps everything else, with the params
	// nested under "params".
	metadata := map[string]any{
		constants.MetadataKeyComponentType: req.ComponentType,
		constants.MetadataKeyModel:         req.Model,
		constants.MetadataKeyParams:        catalogutils.StripSensitiveFields(req.Params, sensitive),
	}

	return &types.RemotePlan{ConnectorID: connectorID, Req: req, Route: r, Metadata: metadata}, nil
}

// remoteTokenizeURL returns the target of the tokenize passthrough for a remote provider, or ""
// when none applies. watsonx has a fixed tokenization endpoint; every other provider is assumed to
// be OpenAI-style, so the target is derived from its api_base.
func remoteTokenizeURL(providerID string, params map[string]any) string {
	if providerID == constants.ConnectorProviderWatsonx {
		return watsonxTokenizeURL
	}

	if apiBase, _ := params[paramAPIBase].(string); apiBase != "" {
		return tokenizeURL(apiBase)
	}

	return ""
}

// tokenizeURL derives the /tokenize target from an OpenAI-style api_base.
func tokenizeURL(apiBase string) string {
	return strings.TrimSuffix(strings.TrimRight(apiBase, "/"), "/v1") + "/tokenize"
}
