package modelphase

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	modeltypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeModels records the phases the deployer runs, in order, and can fail a model's phase.
type fakeModels struct {
	types.ModelManager

	mu         sync.Mutex
	calls      []string
	failDown   string // name of the model whose download fails
	failDeploy string // name of the model whose deploy fails
	gatewayErr error
}

func (f *fakeModels) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, call)
}

func (f *fakeModels) DownloadLocalModel(_ context.Context, p *modeltypes.LocalPlan) error {
	f.record("download:" + p.Req.Name)
	if p.Req.Name == f.failDown {
		return errors.New("disk full")
	}

	return nil
}

func (f *fakeModels) DeployLocalModel(_ context.Context, p *modeltypes.LocalPlan) error {
	f.record("deploy:" + p.Req.Name)
	if p.Req.Name == f.failDeploy {
		return errors.New("no spyre cards")
	}

	return nil
}

func (f *fakeModels) FailLocalModel(_ context.Context, p *modeltypes.LocalPlan, cause error) {
	f.record("fail:" + p.Req.Name + ":" + cause.Error())
}

func (f *fakeModels) GatewayValues(_ context.Context, a modeltypes.ModelAccess) (map[string]any, error) {
	f.record("gateway:" + a.Model)

	if f.gatewayErr != nil {
		return nil, f.gatewayErr
	}

	return map[string]any{"host": "litellm", "port": "4000", "key": "sk-" + a.Model}, nil
}

func modelComp(hash, name string, services ...string) *types.ComponentPlan {
	return &types.ComponentPlan{
		Hash:           hash,
		ComponentType:  "llm",
		DatabaseID:     uuid.New(),
		UsedByServices: services,
		Params:         map[string]any{constants.ParamKeyModel: name},
		Model:          &modeltypes.LocalPlan{Req: apimodels.CreateModelRequest{Name: name}},
	}
}

func TestDownload(t *testing.T) {
	plan := &types.DeploymentPlan{Components: map[string]*types.ComponentPlan{
		"a": modelComp("a", "model-a"),
		"b": modelComp("b", "model-b"),
		"x": {Hash: "x", ComponentType: "vector_store"},
	}}

	t.Run("downloads every model in hash order", func(t *testing.T) {
		fake := &fakeModels{}
		d := New(fake)

		require.NoError(t, d.Download(t.Context(), plan))
		assert.Equal(t, []string{"download:model-a", "download:model-b"}, fake.calls)
	})

	t.Run("stops at the first failure and leaves reporting to the caller", func(t *testing.T) {
		fake := &fakeModels{failDown: "model-a"}
		d := New(fake)

		err := d.Download(t.Context(), plan)

		require.ErrorContains(t, err, "disk full")
		assert.Equal(t, []string{"download:model-a"}, fake.calls)
	})

	t.Run("nothing to do without models", func(t *testing.T) {
		fake := &fakeModels{}
		d := New(fake)

		require.NoError(t, d.Download(t.Context(), &types.DeploymentPlan{}))
		assert.Empty(t, fake.calls)
	})
}

func newDeployPlan() *types.DeploymentPlan {
	return &types.DeploymentPlan{
		Components: map[string]*types.ComponentPlan{
			"a": modelComp("a", "model-a", "chat"),
			"b": modelComp("b", "model-b", "chat"),
			"p": {Hash: "p", ComponentType: "embedding", PreDeployed: true, UsedByServices: []string{"chat"}},
		},
		Services: map[string]*types.ServicePlan{"chat": {CatalogID: "chat", Values: map[string]any{}}},
	}
}

func TestDeployModel(t *testing.T) {
	t.Run("deploys the model and gives the services using it the gateway values", func(t *testing.T) {
		fake := &fakeModels{}
		plan := newDeployPlan()
		comp := plan.Components["a"]

		require.NoError(t, New(fake).DeployModel(t.Context(), plan, comp))

		assert.Equal(t, []string{"deploy:model-a", "gateway:model-a"}, fake.calls)

		values := plan.Services["chat"].Values
		assert.Equal(t, map[string]any{"host": "litellm", "port": "4000", "key": "sk-model-a"}, values["llm"],
			"the services reach the model through the gateway, not its server")
		assert.NotContains(t, values, "embedding", "only the model itself is merged")
	})

	t.Run("a model that already runs is not started but its services get the gateway values", func(t *testing.T) {
		fake := &fakeModels{}
		plan := newDeployPlan()
		plan.Components["p"].Params = map[string]any{constants.ParamKeyModel: "bge"}

		require.NoError(t, New(fake).DeployModel(t.Context(), plan, plan.Components["p"]))

		assert.Equal(t, []string{"gateway:bge"}, fake.calls)
		assert.Subset(t, plan.Services["chat"].Values["embedding"], map[string]any{"host": "litellm", "key": "sk-bge"})
	})

	t.Run("a start failure fails the model and gives the services nothing", func(t *testing.T) {
		fake := &fakeModels{failDeploy: "model-a"}
		plan := newDeployPlan()

		err := New(fake).DeployModel(t.Context(), plan, plan.Components["a"])

		require.ErrorContains(t, err, "no spyre cards")
		assert.Equal(t, []string{
			"deploy:model-a",
			"fail:model-a:failed to deploy model model-a: no spyre cards",
		}, fake.calls)
		assert.Empty(t, plan.Services["chat"].Values)
	})

	t.Run("a gateway failure fails the deployment but not the model that runs", func(t *testing.T) {
		fake := &fakeModels{gatewayErr: errors.New("litellm down")}
		plan := newDeployPlan()

		err := New(fake).DeployModel(t.Context(), plan, plan.Components["a"])

		require.ErrorContains(t, err, "litellm down")
		assert.Equal(t, []string{"deploy:model-a", "gateway:model-a"}, fake.calls, "a running model is left as it is")
	})
}

func TestFail(t *testing.T) {
	fake := &fakeModels{}
	plan := &types.DeploymentPlan{Components: map[string]*types.ComponentPlan{
		"a": modelComp("a", "model-a"),
		"b": modelComp("b", "model-b"),
		"p": {Hash: "p", PreDeployed: true},
	}}

	New(fake).Fail(t.Context(), plan, errors.New("boom"))

	assert.Equal(t, []string{"fail:model-a:boom", "fail:model-b:boom"}, fake.calls, "an existing instance is not failed")
}
