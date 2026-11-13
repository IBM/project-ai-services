package deployment

import (
	"context"
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPlanner(t *testing.T) *DeploymentPlanner {
	t.Helper()

	base, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)
	p, err := base.WithRuntime("podman")
	require.NoError(t, err)

	return NewDeploymentPlanner(p, nil, nil)
}

func newPlan() *types.DeploymentPlan {
	return &types.DeploymentPlan{
		RuntimeType: "podman",
		Components:  map[string]*types.ComponentPlan{},
	}
}

func llmComp(model string, params map[string]any) apimodels.Component {
	return apimodels.Component{ComponentType: "llm", ProviderID: "vllm-spyre", Version: "1.0.0", Model: model, Params: params}
}

func TestProcessComponent_Model(t *testing.T) {
	const (
		granite33 = "ibm-granite/granite-3.3-8b-instruct"
		granite41 = "ibm-granite/granite-4.1-8b-fp8"
	)

	t.Run("same model from two services shares one component", func(t *testing.T) {
		p, plan := newTestPlanner(t), newPlan()

		h1, err := p.processComponent(llmComp(granite33, nil), "chat", plan)
		require.NoError(t, err)
		h2, err := p.processComponent(llmComp(granite33, nil), "summarize", plan)
		require.NoError(t, err)

		assert.Equal(t, h1, h2)
		require.Len(t, plan.Components, 1)
		assert.Equal(t, []string{"chat", "summarize"}, plan.Components[h1].UsedByServices)
	})

	t.Run("different top-level models never share a component", func(t *testing.T) {
		p, plan := newTestPlanner(t), newPlan()

		h1, err := p.processComponent(llmComp(granite33, nil), "chat", plan)
		require.NoError(t, err)
		h2, err := p.processComponent(llmComp(granite41, nil), "summarize", plan)
		require.NoError(t, err)

		assert.NotEqual(t, h1, h2)
		assert.Len(t, plan.Components, 2)
	})

	t.Run("top-level and legacy params.model for the same model share a component", func(t *testing.T) {
		p, plan := newTestPlanner(t), newPlan()

		h1, err := p.processComponent(llmComp(granite33, nil), "chat", plan)
		require.NoError(t, err)
		h2, err := p.processComponent(llmComp("", map[string]any{"model": granite33}), "summarize", plan)
		require.NoError(t, err)

		assert.Equal(t, h1, h2)
		assert.Len(t, plan.Components, 1)
	})

	t.Run("plan params carry the top-level model so downstream readers see it", func(t *testing.T) {
		p, plan := newTestPlanner(t), newPlan()

		h, err := p.processComponent(llmComp(granite33, map[string]any{"apiKey": "k"}), "chat", plan)
		require.NoError(t, err)

		assert.Equal(t, map[string]any{"model": granite33, "apiKey": "k"}, plan.Components[h].Params)
	})
}

func TestProcessService_ProfileReachesComponentPlan(t *testing.T) {
	const granite33 = "ibm-granite/granite-3.3-8b-instruct"

	p, plan := newTestPlanner(t), newPlan()
	plan.Services = map[string]*types.ServicePlan{}

	svc := apimodels.Service{
		CatalogID: "summarize",
		Version:   "1.0.0",
		Components: []apimodels.Component{
			llmComp(granite33, nil),
		},
	}
	require.NoError(t, p.processService(context.Background(), svc, plan))
	require.Len(t, plan.Components, 1)

	for _, comp := range plan.Components {
		assert.Equal(t, "standard", comp.Values["profile"])
		assert.Equal(t, 32768, comp.Values["maxModelLen"])
		assert.Equal(t, granite33, comp.Values["model"])
	}
}
