package openshift

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/mock"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func newTestPlan(t *testing.T, componentType, providerID string) *types.LocalPlan {
	t.Helper()

	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)

	scoped, err := provider.WithRuntime(runtimeTypes.RuntimeTypeOpenShift.String())
	require.NoError(t, err)

	id := uuid.New()

	return &types.LocalPlan{
		ComponentID: id,
		Req:         apimodels.CreateModelRequest{ComponentType: componentType, ProviderID: providerID},
		Runtime:     mock.New(runtimeTypes.RuntimeTypeOpenShift),
		Catalog:     scoped,
		Values: map[string]any{
			catalogconstants.InstanceSlugKey: catalogutils.GenerateInstanceSlug(id.String()),
		},
	}
}

func TestOpenShiftDeployer_Check(t *testing.T) {
	require.NoError(t, NewOpenShiftDeployer(&fakeRoutes{}).Check(context.Background(), newTestPlan(t, "llm", "vllm-cpu")))
}

func TestOpenShiftDeployer_Start_UnknownProvider(t *testing.T) {
	eps, err := NewOpenShiftDeployer(&fakeRoutes{}).Start(context.Background(), newTestPlan(t, "llm", "no-such-provider"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get filesystem for component llm/no-such-provider")
	assert.Equal(t, types.ModelEndpoints{}, eps)
}

func TestRouteEndpoints(t *testing.T) {
	id := uuid.New()
	address := "llm-abc-predictor.ai-services-models.svc.cluster.local:8000"

	t.Run("the local worker keeps the predictor address and gets no routes", func(t *testing.T) {
		routes := &fakeRoutes{}
		got, err := NewOpenShiftDeployer(routes).routeEndpoints(context.Background(), &types.LocalPlan{ComponentID: id, WorkerName: "Local"}, address)
		require.NoError(t, err)
		assert.Equal(t, types.ModelEndpoints{Internal: address, Egress: address}, got)
		assert.False(t, routes.called)
	})

	t.Run("a remote worker returns the control-plane egress", func(t *testing.T) {
		routes := &fakeRoutes{egress: "ai-services--caddy:8080/worker/w1/models/x"}
		got, err := NewOpenShiftDeployer(routes).routeEndpoints(context.Background(), &types.LocalPlan{ComponentID: id, WorkerName: "w1"}, address)
		require.NoError(t, err)
		assert.Equal(t, types.ModelEndpoints{Internal: address, Egress: "ai-services--caddy:8080/worker/w1/models/x"}, got)
		assert.Equal(t, "w1", routes.gotWorker)
		assert.Equal(t, id, routes.gotID)
		assert.Equal(t, address, routes.gotUpstrm)
	})

	t.Run("a routing failure fails the deploy", func(t *testing.T) {
		routes := &fakeRoutes{registerEr: errors.New("caddy down")}
		_, err := NewOpenShiftDeployer(routes).routeEndpoints(context.Background(), &types.LocalPlan{ComponentID: id, WorkerName: "w1"}, address)
		require.ErrorContains(t, err, "caddy down")
		require.ErrorContains(t, err, `worker "w1"`)
	})
}
