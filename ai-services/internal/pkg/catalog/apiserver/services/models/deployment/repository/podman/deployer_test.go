package podman

import (
	"testing"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
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
