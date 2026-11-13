package openshift

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/mock"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPlan(t *testing.T, componentType, providerID string) *types.LocalPlan {
	t.Helper()

	provider, err := catalog.NewCatalogProvider(nil)
	require.NoError(t, err)

	scoped, err := provider.WithRuntime(runtimeTypes.RuntimeTypeOpenShift.String())
	require.NoError(t, err)

	return &types.LocalPlan{
		ComponentID: uuid.New(),
		Req:         apimodels.CreateModelRequest{ComponentType: componentType, ProviderID: providerID},
		Runtime:     mock.New(runtimeTypes.RuntimeTypeOpenShift),
		Catalog:     scoped,
		Values:      map[string]any{},
	}
}

func TestOpenShiftDeployer_Check(t *testing.T) {
	require.NoError(t, NewOpenShiftDeployer().Check(context.Background(), newTestPlan(t, "llm", "vllm-cpu")))
}

func TestOpenShiftDeployer_Start_UnknownProvider(t *testing.T) {
	eps, err := NewOpenShiftDeployer().Start(context.Background(), newTestPlan(t, "llm", "no-such-provider"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get filesystem for component llm/no-such-provider")
	assert.Equal(t, types.ModelEndpoints{}, eps)
}
