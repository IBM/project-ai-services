package podman

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/accelerator"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// freeCardsRuntime is a runtime whose worker reports a number of free Spyre cards; any other
// method would panic if called.
type freeCardsRuntime struct {
	runtime.Runtime

	free int
}

func (f freeCardsRuntime) FindFreeSpyreCards(context.Context) ([]string, error) {
	cards := make([]string, f.free)
	for i := range cards {
		cards[i] = fmt.Sprintf("0000:00:%02d.0", i)
	}

	return cards, nil
}

// newSpyrePlan is a vllm-spyre model plan with the values the planner resolves for it.
func newSpyrePlan(t *testing.T) *types.LocalPlan {
	t.Helper()

	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)

	scoped, err := provider.WithRuntime("podman")
	require.NoError(t, err)

	req := apimodels.CreateModelRequest{
		ComponentType: "llm",
		ProviderID:    "vllm-spyre",
		Model:         "ibm-granite/granite-3.3-8b-instruct",
		Profile:       "standard",
	}

	values, err := scoped.LoadComponentValuesForModel(req.ComponentType, req.ProviderID, req.Model, req.Profile, nil)
	require.NoError(t, err)

	values["model"] = req.Model

	return &types.LocalPlan{ComponentID: uuid.New(), Req: req, Catalog: scoped, Values: values}
}

func TestPodmanDeployerCheck(t *testing.T) {
	plan, other := newSpyrePlan(t), newSpyrePlan(t)

	tmpls, err := loadTemplates(plan)
	require.NoError(t, err)

	each, err := countSpyreCards(t.Context(), plan.Catalog, tmpls, plan.Values)
	require.NoError(t, err)
	require.Positive(t, each, "the vllm-spyre profile must need cards for this test to mean anything")

	check := func(free int, plans ...*types.LocalPlan) error {
		return NewPodmanDeployer(nil).Check(t.Context(), freeCardsRuntime{free: free}, plans)
	}

	t.Run("a model that fits", func(t *testing.T) {
		require.NoError(t, check(each, plan))
	})

	t.Run("a model one card short fails", func(t *testing.T) {
		var insufficient *accelerator.InsufficientError

		require.ErrorAs(t, check(each-1, plan), &insufficient)
		assert.Equal(t, accelerator.InsufficientError{Type: accelerator.Spyre, Need: each, Have: each - 1}, *insufficient)
	})

	t.Run("models that fit alone but not together fail", func(t *testing.T) {
		require.NoError(t, check(each, plan))
		require.NoError(t, check(each, other))

		var insufficient *accelerator.InsufficientError

		require.ErrorAs(t, check(each, plan, other), &insufficient)
		assert.Equal(t, 2*each, insufficient.Need)
		assert.NoError(t, check(2*each, plan, other))
	})

	t.Run("no models need nothing", func(t *testing.T) {
		require.NoError(t, check(0))
	})
}

func TestInternalEndpoint(t *testing.T) {
	plan := &types.LocalPlan{
		ComponentID: uuid.New(),
		Req:         apimodels.CreateModelRequest{ComponentType: "reranker", ProviderID: "vllm-cpu"},
	}

	t.Run("uses the port declared by the pod template", func(t *testing.T) {
		got, err := internalEndpoint(plan, map[string]any{
			"reranker": map[string]any{"host": "reranker-abc", "port": "8002"},
		})
		require.NoError(t, err)
		assert.Equal(t, "reranker-abc:8002", got)
	})

	t.Run("fails instead of guessing a port", func(t *testing.T) {
		_, err := internalEndpoint(plan, map[string]any{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reranker/vllm-cpu")
	})
}

// fakeRoutes is a RouteRegistrar that records the call.
type fakeRoutes struct {
	called     bool
	gotWorker  string
	gotID      uuid.UUID
	gotUpstrm  string
	egress     string
	registerEr error
}

func (f *fakeRoutes) Register(_ context.Context, _ runtime.Runtime, worker string, id uuid.UUID, upstream string) (string, error) {
	f.called, f.gotWorker, f.gotID, f.gotUpstrm = true, worker, id, upstream

	return f.egress, f.registerEr
}

func TestRouteEndpoints(t *testing.T) {
	id := uuid.New()

	t.Run("the local worker keeps the pod address and gets no routes", func(t *testing.T) {
		routes := &fakeRoutes{}
		got, err := NewPodmanDeployer(routes).routeEndpoints(context.Background(), &types.LocalPlan{ComponentID: id, WorkerName: "Local"}, "pod:8000")
		require.NoError(t, err)
		assert.Equal(t, types.ModelEndpoints{Internal: "pod:8000", Egress: "pod:8000"}, got)
		assert.False(t, routes.called)
	})

	t.Run("a remote worker keeps the pod as Internal and returns the control-plane egress", func(t *testing.T) {
		routes := &fakeRoutes{egress: "ai-services--caddy:8080/worker/w1/models/x"}
		got, err := NewPodmanDeployer(routes).routeEndpoints(context.Background(), &types.LocalPlan{ComponentID: id, WorkerName: "w1"}, "pod:8000")
		require.NoError(t, err)
		assert.Equal(t, types.ModelEndpoints{Internal: "pod:8000", Egress: "ai-services--caddy:8080/worker/w1/models/x"}, got)
		assert.True(t, routes.called)
		assert.Equal(t, "w1", routes.gotWorker)
		assert.Equal(t, id, routes.gotID)
		assert.Equal(t, "pod:8000", routes.gotUpstrm, "the worker route targets the pod's own address")
	})

	t.Run("a routing failure fails the deploy instead of returning an unreachable address", func(t *testing.T) {
		routes := &fakeRoutes{registerEr: errors.New("caddy down")}
		_, err := NewPodmanDeployer(routes).routeEndpoints(context.Background(), &types.LocalPlan{ComponentID: id, WorkerName: "w1"}, "pod:8000")
		require.ErrorContains(t, err, "caddy down")
		require.ErrorContains(t, err, `worker "w1"`)
	})
}
