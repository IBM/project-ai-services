package model

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

func TestRenderModelInfo_Local(t *testing.T) {
	d := &models.ModelDetail{
		ModelBase: models.ModelBase{
			ID:                "id-1",
			DeploymentType:    "local",
			Name:              "granite-llm",
			ComponentType:     types.ComponentType{ID: "llm", Name: "Large Language Model (LLM)"},
			Provider:          types.ProviderInfo{ID: "vllm-spyre", Name: "vLLM (Spyre)"},
			Worker:            &types.ApplicationWorker{Name: "lpar-1", RuntimeType: "podman"},
			Model:             "ibm-granite/granite-3.3-8b-instruct",
			Status:            "Running",
			ConnectedServices: 1,
			CreatedAt:         time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC),
		},
		Endpoints:    []models.ModelEndpoint{{Type: "external", URL: "https://gw/v1"}, {Type: "empty"}},
		Applications: []models.ModelApplication{{ID: "a", Name: "my-rag-app"}},
	}

	out := renderModelInfo(d)

	assert.Contains(t, out, "Name:               granite-llm")
	assert.Contains(t, out, "Worker:             lpar-1 (podman)")
	assert.Contains(t, out, "Created At:         2026-07-01T10:00:00Z")
	assert.Contains(t, out, "Updated At:         -")
	assert.Contains(t, out, "Message:            -")
	assert.Contains(t, out, "  - external: https://gw/v1")
	assert.NotContains(t, out, "empty:")
	assert.Contains(t, out, "Applications:\n  - my-rag-app")
	assert.NotContains(t, out, "Parameters:")
}

func TestRenderModelInfo_RemoteNoWorkerNoApps(t *testing.T) {
	d := &models.ModelDetail{
		ModelBase: models.ModelBase{
			Name:           "prod-watsonx",
			DeploymentType: "remote",
			Params:         map[string]any{"project_id": "p1", "api_base": "https://x"},
		},
	}

	out := renderModelInfo(d)

	assert.Contains(t, out, "Worker:             -")
	assert.Contains(t, out, "Parameters:\n  api_base: https://x\n  project_id: p1")
	assert.NotContains(t, out, "Endpoints:")
	assert.NotContains(t, out, "Applications:")
	assert.False(t, strings.HasSuffix(out, "\n"))
}
