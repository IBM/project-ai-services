package modelservice

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDeployer stands in for a runtime-specific types.Deployer.
type fakeDeployer struct {
	endpoint string
	err      error
	panics   bool
	called   chan struct{}
}

func (f *fakeDeployer) Deploy(context.Context, *types.LocalPlan) (string, error) {
	defer close(f.called)

	if f.panics {
		panic("boom")
	}

	return f.endpoint, f.err
}

// podmanRuntime reports the Podman runtime type; every other method would panic if called.
type podmanRuntime struct{ runtime.Runtime }

func (podmanRuntime) Type() runtimeTypes.RuntimeType { return runtimeTypes.RuntimeTypePodman }

func newLocalService(t *testing.T, d types.Deployer, lite *mockLiteLLMClient) (*ModelService, *mockComponentRepo) {
	t.Helper()

	repo := newMockComponentRepo()
	svc := NewModelService(repo, newMockConnectorRepo(), nil, nil, setupTestCatalogProvider(t), lite)
	svc.planner.
		WithDeployers(map[runtimeTypes.RuntimeType]types.Deployer{runtimeTypes.RuntimeTypePodman: d}).
		WithRuntimeResolver(func(string) (runtime.Runtime, error) { return podmanRuntime{}, nil })

	return svc, repo
}

func localRequest() apimodels.CreateModelRequest {
	return apimodels.CreateModelRequest{
		DeploymentType: apimodels.DeploymentTypeLocal,
		ComponentType:  "llm",
		Name:           "local-granite",
		ProviderID:     "vllm-cpu",
		Model:          "ibm-granite/granite-3.3-8b-instruct",
		CreatedBy:      "test-user",
	}
}

// awaitStatus polls until the component reaches want; the deployment runs in the background.
func awaitStatus(t *testing.T, repo *mockComponentRepo, id string, want dbmodels.ComponentStatus) *dbmodels.Component {
	t.Helper()

	cid, err := uuid.Parse(id)
	require.NoError(t, err)

	var comp *dbmodels.Component
	require.Eventually(t, func() bool {
		comp = repo.get(cid)

		return comp != nil && comp.Status == want
	}, 5*time.Second, 10*time.Millisecond, "component never reached status %q", want)

	return comp
}

func TestCreateLocal_Success_RegistersRouteAndMarksRunning(t *testing.T) {
	lite := &mockLiteLLMClient{}
	deployer := &fakeDeployer{endpoint: "llm-abc:8000", called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, lite)

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	comp := awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusRunning)
	assert.Equal(t, []map[string]any{{"type": "internal", "url": "http://llm-abc:8000"}}, comp.Endpoints)

	require.Len(t, lite.registeredModels, 1)
	assert.Equal(t, "hosted_vllm/ibm-granite/granite-3.3-8b-instruct", lite.registeredModels[0].LiteLLMParams.Model)
	assert.Equal(t, "http://llm-abc:8000/v1", lite.registeredModels[0].LiteLLMParams.APIBase)
	assert.Equal(t, []string{resp.ID}, lite.registeredModels[0].LiteLLMParams.Tags)
	require.Len(t, lite.registeredPass, 1)
	assert.Equal(t, "http://llm-abc:8000/tokenize", lite.registeredPass[0].Target)
}

func TestCreateLocal_DeployFailure_MarksError(t *testing.T) {
	lite := &mockLiteLLMClient{}
	deployer := &fakeDeployer{err: errors.New("image pull failed"), called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, lite)

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	comp := awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusError)
	assert.Contains(t, comp.Message, "image pull failed")
	assert.Empty(t, lite.registeredModels, "no route must be registered when pods failed to deploy")
}

func TestCreateLocal_Panic_MarksError(t *testing.T) {
	deployer := &fakeDeployer{panics: true, called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, &mockLiteLLMClient{})

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	comp := awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusError)
	assert.Contains(t, comp.Message, "panic")
}

func TestCreateLocal_ProbeFailure_RollsBackRouteAndMarksError(t *testing.T) {
	lite := &mockLiteLLMClient{probeErr: errors.New("connection refused")}
	deployer := &fakeDeployer{endpoint: "llm-abc:8000", called: make(chan struct{})}
	svc, repo := newLocalService(t, deployer, lite)

	resp, err := svc.CreateModel(context.Background(), localRequest())
	require.NoError(t, err)

	comp := awaitStatus(t, repo, resp.ID, dbmodels.ComponentStatusError)
	assert.Contains(t, comp.Message, "connection refused")
	assert.Equal(t, []string{"ibm-granite/granite-3.3-8b-instruct"}, lite.deregisteredModels)
	assert.Len(t, lite.deregisteredPass, 1)
}

// The plan phase must reject bad requests before any row exists or any background work starts.
func TestCreateLocal_PlanFailures_HaveNoSideEffects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*apimodels.CreateModelRequest)
		code   int
	}{
		{"unknown provider", func(r *apimodels.CreateModelRequest) { r.ProviderID = "nope" }, http.StatusNotFound},
		{"params not matching schema", func(r *apimodels.CreateModelRequest) { r.Params = map[string]any{"bogus": "x"} }, http.StatusBadRequest},
		{"unknown worker", func(r *apimodels.CreateModelRequest) { r.WorkerSelector = "ghost" }, http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployer := &fakeDeployer{called: make(chan struct{})}
			svc, repo := newLocalService(t, deployer, &mockLiteLLMClient{})

			req := localRequest()
			tt.mutate(&req)

			resp, err := svc.CreateModel(context.Background(), req)
			assert.Nil(t, resp)
			requireStatus(t, err, tt.code, "")
			assert.Empty(t, repo.components, "plan failures must not write a component row")

			select {
			case <-deployer.called:
				t.Fatal("deployer must not run when planning fails")
			default:
			}
		})
	}
}

func TestPlanLocal_UnsupportedRuntime(t *testing.T) {
	svc, repo := newLocalService(t, &fakeDeployer{called: make(chan struct{})}, &mockLiteLLMClient{})
	svc.planner.WithDeployers(map[runtimeTypes.RuntimeType]types.Deployer{}) // e.g. OpenShift: no deployer registered

	_, err := svc.CreateModel(context.Background(), localRequest())
	requireStatus(t, err, http.StatusUnprocessableEntity, "unsupported")
	assert.Empty(t, repo.components)
}

func TestPlanLocal_ValuesCarryParamsAndModel(t *testing.T) {
	svc, _ := newLocalService(t, &fakeDeployer{called: make(chan struct{})}, &mockLiteLLMClient{})

	req := localRequest()
	req.Params = map[string]any{"apiKey": "k"}

	plan, err := svc.planner.PlanLocal(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, "k", plan.Values["apiKey"])
	assert.Equal(t, req.Model, plan.Values["model"], "the requested model must reach the templates as .Values.model")
	assert.NotEqual(t, uuid.Nil, plan.ComponentID)
}

func TestPlanRemote_StripsSensitiveFromMetadata(t *testing.T) {
	svc, _ := newLocalService(t, &fakeDeployer{called: make(chan struct{})}, &mockLiteLLMClient{})

	plan, err := svc.planner.PlanRemote(context.Background(), apimodels.CreateModelRequest{
		DeploymentType: apimodels.DeploymentTypeRemote,
		ComponentType:  "embedding",
		Name:           "remote-vllm",
		ProviderID:     "hosted_vllm",
		Model:          "ibm/embed",
		Params:         map[string]any{"api_base": "http://vllm.example.com:8000/v1", "api_key": "secret"},
	})
	require.NoError(t, err)

	assert.NotContains(t, plan.Metadata, "api_key")
	assert.Equal(t, "http://vllm.example.com:8000/v1", plan.Metadata["api_base"])
	assert.Equal(t, "embedding", plan.Metadata["component_type"])
	assert.Equal(t, "secret", plan.Route.APIKey, "the gateway still receives the credential")
	assert.Equal(t, "http://vllm.example.com:8000/tokenize", plan.Route.TokenizeURL)
}

func requireStatus(t *testing.T, err error, code int, msgContains string) {
	t.Helper()

	var valErr *validators.ValidationError
	require.ErrorAs(t, err, &valErr)
	assert.Equal(t, code, valErr.Code)
	assert.Contains(t, valErr.Message, msgContains)
}
