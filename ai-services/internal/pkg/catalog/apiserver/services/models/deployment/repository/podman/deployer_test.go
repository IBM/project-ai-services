package podman

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
