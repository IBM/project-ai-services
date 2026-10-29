package modelservice

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	texttemplate "text/template"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	deploymenttypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	podmanutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/utils/podman"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// ModelService implements the ModelServiceInterface for model management.
type ModelService struct {
	componentRepo   dbrepo.ComponentRepository
	connectorRepo   dbrepo.ConnectorRepository
	workerRepo      dbrepo.WorkerRepository
	workerRegistry  stream.WorkerRegistry
	catalogProvider *catalog.CatalogProvider
	litellmClient   catalogclient.LiteLLMClient
	// runtimeResolver overrides resolveRuntime; used by tests.
	runtimeResolver func(workerName string) (runtime.Runtime, error)
}

// NewModelService creates a new ModelService instance.
func NewModelService(
	componentRepo dbrepo.ComponentRepository,
	connectorRepo dbrepo.ConnectorRepository,
	workerRepo dbrepo.WorkerRepository,
	workerRegistry stream.WorkerRegistry,
	catalogProvider *catalog.CatalogProvider,
	litellmClient catalogclient.LiteLLMClient,
) *ModelService {
	if litellmClient == nil {
		litellmClient = catalogclient.NewLiteLLMClient("", "")
	}

	return &ModelService{
		componentRepo:   componentRepo,
		connectorRepo:   connectorRepo,
		workerRepo:      workerRepo,
		workerRegistry:  workerRegistry,
		catalogProvider: catalogProvider,
		litellmClient:   litellmClient,
	}
}

// CreateModel creates a new model (local pod deployment or remote connector).
func (s *ModelService) CreateModel(ctx context.Context, req apimodels.CreateModelRequest) (*apimodels.CreateModelResponse, error) {
	// Validate common fields
	if err := s.validateCommonFields(req); err != nil {
		return nil, err
	}

	// Check name uniqueness across components and connectors
	if err := s.checkNameUniqueness(ctx, req.Name); err != nil {
		return nil, err
	}

	switch req.DeploymentType {
	case "local":
		return s.createLocalModel(ctx, req)
	case "remote":
		return s.createRemoteModel(ctx, req)
	default:
		return nil, &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("invalid deployment_type %q; must be 'local' or 'remote'", req.DeploymentType),
		}
	}
}

func (s *ModelService) validateCommonFields(req apimodels.CreateModelRequest) error {
	if req.DeploymentType != "local" && req.DeploymentType != "remote" {
		return &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("invalid deployment_type %q; must be 'local' or 'remote'", req.DeploymentType),
		}
	}

	if req.ComponentType != "llm" && req.ComponentType != "embedding" && req.ComponentType != "reranker" {
		return &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("invalid component_type %q; must be 'llm', 'embedding', or 'reranker'", req.ComponentType),
		}
	}

	if len(req.Name) < 3 || len(req.Name) > 100 || !nameRegex.MatchString(req.Name) {
		return &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: "name must be between 3 and 100 characters and contain only letters, numbers, hyphens, and underscores",
		}
	}

	if req.ProviderID == "" {
		return &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: "provider_id is required",
		}
	}

	if req.Model == "" {
		return &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: "model is required",
		}
	}

	return nil
}

func (s *ModelService) checkNameUniqueness(ctx context.Context, name string) error {
	if s.componentRepo != nil {
		existingComp, err := s.componentRepo.GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("failed to check component name uniqueness: %w", err)
		}
		if existingComp != nil {
			return &validators.ValidationError{
				Code:    http.StatusConflict,
				Message: fmt.Sprintf("A model with name %q already exists", name),
			}
		}
	}

	if s.connectorRepo != nil {
		existingConn, err := s.connectorRepo.GetByName(ctx, name)
		if err != nil {
			return fmt.Errorf("failed to check connector name uniqueness: %w", err)
		}
		if existingConn != nil {
			return &validators.ValidationError{
				Code:    http.StatusConflict,
				Message: fmt.Sprintf("A connector with name %q already exists", name),
			}
		}
	}

	return nil
}

func (s *ModelService) createLocalModel(ctx context.Context, req apimodels.CreateModelRequest) (*apimodels.CreateModelResponse, error) {
	// Validate provider existence in catalog
	compMeta, err := s.catalogProvider.LoadComponent(req.ComponentType, req.ProviderID)
	if err != nil || compMeta == nil {
		return nil, &validators.ValidationError{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("provider %q not found for component type %q", req.ProviderID, req.ComponentType),
		}
	}

	// Resolve target worker
	var workerID *uuid.UUID
	workerName := workerconstants.LocalWorkerName
	workerRuntimeType := string(runtimeTypes.RuntimeTypePodman)

	if req.WorkerSelector != "" {
		if s.workerRepo == nil {
			return nil, &validators.ValidationError{
				Code:    http.StatusNotFound,
				Message: fmt.Sprintf("worker %q not found", req.WorkerSelector),
			}
		}

		worker, err := s.workerRepo.GetByName(ctx, req.WorkerSelector)
		if err != nil || worker == nil {
			return nil, &validators.ValidationError{
				Code:    http.StatusNotFound,
				Message: fmt.Sprintf("worker %q not found", req.WorkerSelector),
			}
		}

		if string(worker.Status) != "ready" {
			return nil, &validators.ValidationError{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("worker %q is not ready", req.WorkerSelector),
			}
		}

		workerRuntimeType = string(worker.RuntimeType)
		if (workerRuntimeType == "" || workerRuntimeType == string(dbmodels.WorkerRuntimeTypeUnknown)) && s.workerRegistry != nil {
			if regRT, ok := s.workerRegistry.WorkerRuntimeType(worker.Name); ok {
				workerRuntimeType = regRT
			}
		}

		if workerRuntimeType == "openshift" {
			return nil, &validators.ValidationError{
				Code:    http.StatusBadRequest,
				Message: "models are unsupported on openshift runtime",
			}
		}

		workerID = &worker.ID
		workerName = worker.Name
	} else if s.workerRepo != nil {
		if localWorker, err := s.workerRepo.GetByName(ctx, workerconstants.LocalWorkerName); err == nil && localWorker != nil {
			localRuntimeType := string(localWorker.RuntimeType)
			if (localRuntimeType == "" || localRuntimeType == string(dbmodels.WorkerRuntimeTypeUnknown)) && s.workerRegistry != nil {
				if regRT, ok := s.workerRegistry.WorkerRuntimeType(localWorker.Name); ok {
					localRuntimeType = regRT
				}
			}

			if localRuntimeType == "openshift" {
				return nil, &validators.ValidationError{
					Code:    http.StatusBadRequest,
					Message: "models are unsupported on openshift runtime",
				}
			}
			workerRuntimeType = localRuntimeType
			workerID = &localWorker.ID
			workerName = localWorker.Name
		}
	}

	// Resolve the runtime up front so an unreachable worker fails the request (422)
	// instead of leaving a component row stuck in Deploying.
	rt, err := s.resolveRuntime(workerName)
	if err != nil {
		return nil, &validators.ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: fmt.Sprintf("cannot reach worker %q: %v", workerName, err),
		}
	}

	// Persist component in DB with status Deploying
	componentID := uuid.New()
	metadata := map[string]any{
		"model": req.Model,
	}
	if req.Profile != "" {
		metadata["profile"] = req.Profile
	}

	component := &dbmodels.Component{
		ID:        componentID,
		Name:      req.Name,
		Type:      req.ComponentType,
		Provider:  req.ProviderID,
		Status:    dbmodels.ComponentStatusDeploying,
		CreatedBy: req.CreatedBy,
		WorkerID:  workerID,
		Metadata:  metadata,
	}

	if s.componentRepo != nil {
		if err := s.componentRepo.Insert(ctx, component); err != nil {
			return nil, fmt.Errorf("failed to insert component: %w", err)
		}
	}

	// Launch async deployment in background
	go s.deployLocalModelAsync(context.Background(), componentID, req, workerName, rt)

	return &apimodels.CreateModelResponse{
		ID: componentID.String(),
	}, nil
}

// resolveRuntime returns the runtime used to deploy to the named worker.
// Connected workers (local or remote) are reached over the gRPC CommandStream. When the local
// worker daemon is not connected, it falls back to a directly created local runtime; Spyre card
// discovery is unavailable in that case, but providers that need no cards (e.g. vllm-cpu) work.
func (s *ModelService) resolveRuntime(workerName string) (runtime.Runtime, error) {
	if s.runtimeResolver != nil {
		return s.runtimeResolver(workerName)
	}

	if s.workerRegistry != nil {
		if workerRT, ok := s.workerRegistry.WorkerRuntimeType(workerName); ok {
			return runtime.NewRuntimeFactory(runtimeTypes.RuntimeType(workerRT)).CreateRemote(workerName, s.workerRegistry, "")
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

func (s *ModelService) deployLocalModelAsync(
	ctx context.Context,
	componentID uuid.UUID,
	req apimodels.CreateModelRequest,
	workerName string,
	rt runtime.Runtime,
) {
	defer func() {
		if r := recover(); r != nil {
			logger.ErrorfCtx(ctx, "Panic in model deployment: %v", r)
			if s.componentRepo != nil {
				_ = s.componentRepo.UpdateStatus(ctx, componentID, dbmodels.ComponentStatusError, fmt.Sprintf("Panic: %v", r))
			}
		}
	}()

	switch rt.Type() {
	case runtimeTypes.RuntimeTypeOpenShift:
		s.failLocalDeployment(ctx, componentID, "models are unsupported on openshift runtime")

		return
	case runtimeTypes.RuntimeTypePodman:
		s.deployPodmanLocalModel(ctx, componentID, req, workerName, rt)
	default:
		s.failLocalDeployment(ctx, componentID, fmt.Sprintf("unsupported runtime type %q for model deployment", rt.Type()))

		return
	}
}

func (s *ModelService) deployPodmanLocalModel(
	ctx context.Context,
	componentID uuid.UUID,
	req apimodels.CreateModelRequest,
	workerName string,
	rt runtime.Runtime,
) {
	scopedProvider, err := s.catalogProvider.WithRuntime(runtimeTypes.RuntimeTypePodman.String())
	if err != nil {
		s.failLocalDeployment(ctx, componentID, fmt.Sprintf("failed to scope catalog provider: %v", err))

		return
	}

	metadata, err := scopedProvider.LoadComponentRuntimeMetadata(req.ComponentType, req.ProviderID)
	if err != nil {
		s.failLocalDeployment(ctx, componentID, fmt.Sprintf("failed to load component metadata: %v", err))

		return
	}

	tmpls, err := scopedProvider.LoadComponentTemplates(req.ComponentType, req.ProviderID)
	if err != nil {
		s.failLocalDeployment(ctx, componentID, fmt.Sprintf("failed to load component templates: %v", err))

		return
	}

	values, err := scopedProvider.LoadComponentValues(req.ComponentType, req.ProviderID, map[string]string{})
	if err != nil || values == nil {
		values = make(map[string]any)
	}

	for k, v := range req.Params {
		values[k] = v
	}
	values["model"] = req.Model
	if req.Profile != "" {
		values["profile"] = req.Profile
	}

	if err := s.preparePrerequisites(ctx, componentID, req, scopedProvider, tmpls, values, rt); err != nil {
		s.failLocalDeployment(ctx, componentID, err.Error())

		return
	}

	// Allocate Spyre cards (no-op for providers that declare none) before rendering the pods.
	spyrePool, err := s.allocateSpyreCards(ctx, scopedProvider, tmpls, values, rt)
	if err != nil {
		s.failLocalDeployment(ctx, componentID, fmt.Sprintf("spyre card allocation failed: %v", err))

		return
	}

	baseDir, err := podmanutils.FetchBaseDir(ctx, rt)
	if err != nil {
		s.failLocalDeployment(ctx, componentID, err.Error())

		return
	}

	// Deploy the pods through the same code path as application components. This handles
	// ordered PodTemplateExecutions, Spyre env injection, the ai-services.io/template label
	// used by deletion, and endpoint extraction.
	podEndpoints, err := podmanutils.DeployComponentPods(ctx, podmanutils.ComponentPodsParams{
		Runtime:       rt,
		ComponentID:   componentID,
		ComponentType: req.ComponentType,
		Values:        values,
		Metadata:      metadata,
		Templates:     tmpls,
		SpyrePool:     spyrePool,
		BaseDir:       baseDir,
	})
	if err != nil {
		s.failLocalDeployment(ctx, componentID, fmt.Sprintf("failed to deploy component pods: %v", err))

		return
	}

	// Internal endpoint is <pod-name>:<port>, falling back to a deterministic name when the
	// templates did not expose a port.
	internalEndpoint := fmt.Sprintf("%s-%s:8000", req.ComponentType, componentID.String()[:10])
	if ep, ok := podEndpoints[req.ComponentType].(map[string]any); ok {
		host, _ := ep["host"].(string)
		port, _ := ep["port"].(string)
		if host != "" && port != "" {
			internalEndpoint = fmt.Sprintf("%s:%s", host, port)
		}
	}

	// Resolve the target endpoint for LiteLLM gateway registration:
	// - For local worker: LiteLLM accesses the model pod via the internal endpoint directly.
	// - For remote worker: the internal pod name is not resolvable from the control plane.
	// NOTE: Remote workers require mTLS routes to be implemented. Mirror the model-manager POC:
	//  1. Register a path-based mTLS ingress route on the worker Caddy (:8443) -> pod:8000.
	//  2. Register the matching mTLS egress route on the control-plane Caddy (:8080), dialing
	//     <worker>.<domain-suffix>:8443 with the control-plane client cert.
	//  3. Point LiteLLM api_base at the control-plane Caddy egress URL instead of the pod name.
	//  4. Unregister both routes on undeploy.
	// Until then, remote worker models are registered with the internal endpoint, which LiteLLM
	// cannot reach across hosts.
	targetEndpoint := internalEndpoint

	// Register route in LiteLLM Gateway
	apiBaseSuffix := "/v1"
	if req.ComponentType == "reranker" {
		apiBaseSuffix = ""
	}
	apiBase := fmt.Sprintf("http://%s%s", targetEndpoint, apiBaseSuffix)

	modelNewReq := catalogclient.LiteLLMModelNewRequest{
		ModelName: req.Model,
		LiteLLMParams: catalogclient.LiteLLMModelParams{
			Model:   fmt.Sprintf("hosted_vllm/%s", req.Model),
			APIBase: apiBase,
			Tags:    []string{componentID.String()},
		},
	}
	if req.ComponentType == "embedding" {
		modelNewReq.ModelInfo = &catalogclient.LiteLLMModelInfo{Mode: "embedding"}
	} else if req.ComponentType == "reranker" {
		modelNewReq.ModelInfo = &catalogclient.LiteLLMModelInfo{Mode: "rerank"}
	}

	if err := s.litellmClient.RegisterModel(ctx, modelNewReq); err != nil {
		logger.ErrorfCtx(ctx, "failed to register model in litellm: %v", err)
	}

	// Register tokenize passthrough
	passReq := catalogclient.LiteLLMPassThroughRequest{
		ID:      fmt.Sprintf("vllm-token-passthrough-%s", componentID.String()),
		Path:    fmt.Sprintf("/tokenize/%s", componentID.String()),
		Target:  fmt.Sprintf("http://%s/tokenize", targetEndpoint),
		Methods: []string{"POST"},
		Auth:    false,
	}
	if err := s.litellmClient.RegisterPassThrough(ctx, passReq); err != nil {
		logger.ErrorfCtx(ctx, "failed to register tokenize passthrough: %v", err)
	}

	// Health probe LiteLLM
	probeResp, probeErr := s.litellmClient.HealthProbe(ctx, req.Model)
	if probeErr != nil || (probeResp != nil && probeResp.HealthyCount == 0 && probeResp.UnhealthyCount > 0) {
		msg := "Health probe failed"
		if probeErr != nil {
			msg = probeErr.Error()
		} else if probeResp != nil && len(probeResp.UnhealthyEndpoints) > 0 {
			msg = probeResp.UnhealthyEndpoints[0].Error
		}
		s.failLocalDeployment(ctx, componentID, msg)

		return
	}

	// Update component status to Running and save internal (and future external) endpoints
	endpoints := []map[string]any{
		{
			"type": "internal",
			"url":  fmt.Sprintf("http://%s", internalEndpoint),
		},
	}
	if s.componentRepo != nil {
		_ = s.componentRepo.UpdateEndpoints(ctx, componentID, endpoints)
		_ = s.componentRepo.UpdateStatus(ctx, componentID, dbmodels.ComponentStatusRunning, "")
	}
	logger.InfofCtx(ctx, "Local model %s (%s) deployed successfully", req.Name, componentID)
}

// allocateSpyreCards builds a Spyre card pool when the provider templates declare spyre-card
// annotations. It returns a nil pool for providers that need no cards.
func (s *ModelService) allocateSpyreCards(
	ctx context.Context,
	scopedProvider *catalog.CatalogProvider,
	tmpls map[string]*texttemplate.Template,
	values map[string]any,
	rt runtime.Runtime,
) (*deploymenttypes.SpyreCardPool, error) {
	required, err := scopedProvider.CollectSpyreCardsFromTemplates(ctx, tmpls, values)
	if err != nil {
		return nil, fmt.Errorf("failed to count required Spyre cards: %w", err)
	}

	if required == 0 {
		return nil, nil
	}

	logger.InfofCtx(ctx, "Model requires %d Spyre card(s)", required)

	return podmanutils.NewSpyreCardPool(ctx, rt, required)
}

// preparePrerequisites pulls the container images declared by the provider templates and
// downloads the model weights on the target worker. It reuses the same podmanutils helpers
// as the application deployment path.
func (s *ModelService) preparePrerequisites(
	ctx context.Context,
	componentID uuid.UUID,
	req apimodels.CreateModelRequest,
	scopedProvider *catalog.CatalogProvider,
	tmpls map[string]*texttemplate.Template,
	values map[string]any,
	rt runtime.Runtime,
) error {
	// Tool image is required for the model download container and housekeeping tasks.
	imageSet := map[string]bool{vars.ToolImage: true}
	if err := scopedProvider.CollectImagesFromTemplates(ctx, tmpls, values, imageSet); err != nil {
		return fmt.Errorf("failed to collect images for %s/%s: %w", req.ComponentType, req.ProviderID, err)
	}

	images := make([]string, 0, len(imageSet))
	for img := range imageSet {
		images = append(images, img)
	}

	logger.InfofCtx(ctx, "Pulling images for model component %s", componentID)
	if err := podmanutils.PullImages(ctx, rt, images); err != nil {
		return fmt.Errorf("image pull failed: %w", err)
	}

	logger.InfofCtx(ctx, "Downloading model %q for component %s", req.Model, componentID)
	if err := podmanutils.DownloadModels(ctx, rt, []string{req.Model}); err != nil {
		return fmt.Errorf("model download failed: %w", err)
	}

	return nil
}

func (s *ModelService) failLocalDeployment(ctx context.Context, componentID uuid.UUID, errMsg string) {
	logger.ErrorfCtx(ctx, "Model deployment failed for %s: %s", componentID, errMsg)
	if s.componentRepo != nil {
		_ = s.componentRepo.UpdateStatus(ctx, componentID, dbmodels.ComponentStatusError, errMsg)
	}
}

func (s *ModelService) createRemoteModel(ctx context.Context, req apimodels.CreateModelRequest) (*apimodels.CreateModelResponse, error) {
	if req.ProviderID != "watsonx" && req.ProviderID != "hosted_vllm" {
		return nil, &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: fmt.Sprintf("unknown provider_id %q for remote model (expected 'watsonx' or 'hosted_vllm')", req.ProviderID),
		}
	}

	if req.Params == nil {
		req.Params = make(map[string]any)
	}

	apiKey, _ := req.Params["api_key"].(string)
	apiBase, _ := req.Params["api_base"].(string)
	projectID, _ := req.Params["project_id"].(string)

	if req.ProviderID == "watsonx" && apiKey == "" {
		return nil, &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: "api_key is required for watsonx provider",
		}
	}

	if req.ProviderID == "hosted_vllm" && apiBase == "" {
		return nil, &validators.ValidationError{
			Code:    http.StatusBadRequest,
			Message: "api_base is required for hosted_vllm provider",
		}
	}

	connectorID := uuid.New()

	// Register route in LiteLLM Gateway
	litellmModel := fmt.Sprintf("%s/%s", req.ProviderID, req.Model)
	modelNewReq := catalogclient.LiteLLMModelNewRequest{
		ModelName: req.Model,
		LiteLLMParams: catalogclient.LiteLLMModelParams{
			Model:     litellmModel,
			APIBase:   apiBase,
			APIKey:    apiKey,
			ProjectID: projectID,
			Tags:      []string{connectorID.String()},
		},
	}
	if req.ComponentType == "embedding" {
		modelNewReq.ModelInfo = &catalogclient.LiteLLMModelInfo{Mode: "embedding"}
	} else if req.ComponentType == "reranker" {
		modelNewReq.ModelInfo = &catalogclient.LiteLLMModelInfo{Mode: "rerank"}
	}

	if err := s.litellmClient.RegisterModel(ctx, modelNewReq); err != nil {
		return nil, fmt.Errorf("failed to register route with litellm: %w", err)
	}

	// Run connectivity health probe
	probeResp, probeErr := s.litellmClient.HealthProbe(ctx, req.Model)
	if probeErr != nil || (probeResp != nil && probeResp.HealthyCount == 0) {
		// Rollback litellm route
		_ = s.litellmClient.DeregisterModel(ctx, req.Model)

		errMsg := "LiteLLM connectivity probe failed"
		if probeErr != nil {
			errMsg = fmt.Sprintf("LiteLLM connectivity probe failed: %v", probeErr)
		} else if probeResp != nil && len(probeResp.UnhealthyEndpoints) > 0 {
			errMsg = fmt.Sprintf("LiteLLM connectivity probe failed: %s", probeResp.UnhealthyEndpoints[0].Error)
		}

		return nil, &validators.ValidationError{
			Code:    http.StatusUnprocessableEntity,
			Message: errMsg,
		}
	}

	// For hosted_vllm, register tokenize passthrough endpoint
	if req.ProviderID == "hosted_vllm" {
		target := strings.TrimSuffix(apiBase, "/v1") + "/tokenize"
		passReq := catalogclient.LiteLLMPassThroughRequest{
			ID:      fmt.Sprintf("vllm-token-passthrough-%s", connectorID.String()),
			Path:    fmt.Sprintf("/tokenize/%s", connectorID.String()),
			Target:  target,
			Methods: []string{"POST"},
			Auth:    false,
		}
		if err := s.litellmClient.RegisterPassThrough(ctx, passReq); err != nil {
			logger.ErrorfCtx(ctx, "failed to register tokenize passthrough for hosted_vllm: %v", err)
		}
	}

	// Prepare non-sensitive metadata (strip apiKey)
	metadata := map[string]any{
		"component_type": req.ComponentType,
		"model_name":     req.Model,
	}
	if apiBase != "" {
		metadata["api_base"] = apiBase
	}
	if projectID != "" {
		metadata["project_id"] = projectID
	}

	connector := &dbmodels.Connector{
		ID:        connectorID,
		Name:      req.Name,
		Type:      "models",
		Provider:  req.ProviderID,
		Status:    dbmodels.ConnectorStatusConnected,
		Message:   "Endpoint reachable and credentials accepted",
		CreatedBy: req.CreatedBy,
		Metadata:  metadata,
	}

	if s.connectorRepo != nil {
		if err := s.connectorRepo.Insert(ctx, connector); err != nil {
			_ = s.litellmClient.DeregisterModel(ctx, req.Model)
			if req.ProviderID == "hosted_vllm" {
				_ = s.litellmClient.DeregisterPassThrough(ctx, fmt.Sprintf("vllm-token-passthrough-%s", connectorID.String()))
			}

			return nil, fmt.Errorf("failed to insert connector: %w", err)
		}
	}

	return &apimodels.CreateModelResponse{
		ID: connectorID.String(),
	}, nil
}
