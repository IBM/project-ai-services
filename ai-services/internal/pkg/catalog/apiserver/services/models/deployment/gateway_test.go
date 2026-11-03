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
	assert.Equal(t, "p/m", req.LiteLLMParams.Model)
	assert.Equal(t, []string{"id-1"}, req.LiteLLMParams.Tags)
	require.NotNil(t, req.ModelInfo)
	assert.Equal(t, "rerank", req.ModelInfo.Mode)

	r.Mode = ""
	assert.Nil(t, modelRequest(r).ModelInfo)
}

func TestPassThroughRequest(t *testing.T) {
	r := types.Route{Tag: "id-1", TokenizeURL: "http://llm:8000/tokenize"}
	req := passThroughRequest(r)

	assert.Equal(t, "vllm-token-passthrough-id-1", req.ID)
	assert.Equal(t, "/tokenize/id-1", req.Path)
	assert.Equal(t, "http://llm:8000/tokenize", req.Target)
	assert.Equal(t, []string{"POST"}, req.Methods)
	assert.True(t, req.Auth, "tokenize passthrough must require LiteLLM authentication")
}
