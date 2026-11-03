package deployment

import (
	"testing"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/models/deployment/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelRequest(t *testing.T) {
	r := types.Route{Tag: "id-1", Mode: "rerank", Model: "m", UpstreamModel: "p/m"}
	req := modelRequest(r)

	assert.Equal(t, "m", req.ModelName)
	assert.Equal(t, "p/m", req.LiteLLMParams["model"])
	assert.Equal(t, []string{"id-1"}, req.LiteLLMParams["tags"])
	require.NotNil(t, req.ModelInfo)
	assert.Equal(t, "rerank", req.ModelInfo.Mode)

	r.Mode = ""
	assert.Nil(t, modelRequest(r).ModelInfo)
}

// Whatever the provider schema allows is forwarded to LiteLLM as is, including fields the
// platform has never heard of; only model and tags are owned by the platform.
func TestModelRequest_ForwardsParamsVerbatim(t *testing.T) {
	params := map[string]any{
		"api_base":   "https://host/v1",
		"api_key":    "secret",
		"project_id": "proj-1",
		"timeout":    30,
		"model":      "attacker/override",
		"tags":       []string{"attacker"},
	}
	r := types.Route{Tag: "id-1", Model: "m", UpstreamModel: "p/m", Params: params}

	got := modelRequest(r).LiteLLMParams

	assert.Equal(t, "https://host/v1", got["api_base"])
	assert.Equal(t, "secret", got["api_key"])
	assert.Equal(t, "proj-1", got["project_id"])
	assert.Equal(t, 30, got["timeout"])
	assert.Equal(t, "p/m", got["model"], "a provider param must not override the platform-owned model")
	assert.Equal(t, []string{"id-1"}, got["tags"], "a provider param must not override the platform-owned tags")
	assert.Equal(t, "attacker/override", params["model"], "the route params must not be mutated")
}

// api_base comes from the platform-derived APIBase for local pods and from Params for remote models.
func TestModelRequest_APIBase(t *testing.T) {
	local := types.Route{Tag: "id-1", Model: "m", UpstreamModel: "p/m", APIBase: "http://llm:8000/v1"}
	assert.Equal(t, "http://llm:8000/v1", modelRequest(local).LiteLLMParams["api_base"])

	remote := types.Route{Tag: "id-1", Model: "m", UpstreamModel: "p/m", Params: map[string]any{"api_base": "https://host"}}
	assert.Equal(t, "https://host", modelRequest(remote).LiteLLMParams["api_base"])

	none := types.Route{Model: "m"}
	assert.NotContains(t, modelRequest(none).LiteLLMParams, "api_base")
}

func TestPassThroughRequest(t *testing.T) {
	r := types.Route{Tag: "id-1", Provider: "watsonx", TokenizeURL: "http://llm:8000/tokenize"}
	req := passThroughRequest(r)

	assert.Equal(t, "watsonx-token-passthrough-id-1", req.ID)
	assert.Equal(t, "/tokenize/id-1", req.Path)
	assert.Equal(t, "http://llm:8000/tokenize", req.Target)
	assert.Equal(t, []string{"POST"}, req.Methods)
	assert.True(t, req.Auth, "tokenize passthrough must require LiteLLM authentication")
}
