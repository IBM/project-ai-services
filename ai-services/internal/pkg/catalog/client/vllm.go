package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	defaultVLLMTimeout = 15 * time.Second

	// vllmModelsPath is the path of the models endpoint, under the server's address.
	vllmModelsPath = "/v1/models"
)

// VLLMModelInfo represents a model returned by vLLM.
type VLLMModelInfo struct {
	ID          string `json:"id"`
	MaxModelLen int    `json:"max_model_len"`
}

// VLLMModelsResponse is the response format for vLLM /v1/models.
type VLLMModelsResponse struct {
	Data []VLLMModelInfo `json:"data"`
}

// VLLMClient defines the client interface for a vLLM server. Unlike the LiteLLM gateway there is
// no single server: every remote model has its own, so the address and key are arguments of each
// call and one client serves them all.
type VLLMClient interface {
	// GetModels lists the models served at apiBase (e.g. http://host:8000, without /v1). apiKey
	// may be empty when the server does not require one.
	GetModels(ctx context.Context, apiBase, apiKey string) (*VLLMModelsResponse, error)
}

type vllmClient struct {
	httpClient *resty.Client
}

// NewVLLMClient creates a new VLLMClient.
func NewVLLMClient() VLLMClient {
	return &vllmClient{
		httpClient: resty.New().
			SetTimeout(defaultVLLMTimeout).
			SetHeader("Content-Type", "application/json"),
	}
}

// GetModels retrieves the list of models from the vLLM server at apiBase.
func (c *vllmClient) GetModels(ctx context.Context, apiBase, apiKey string) (*VLLMModelsResponse, error) {
	req := c.httpClient.R().SetContext(ctx)
	if apiKey != "" {
		req.SetAuthToken(apiKey)
	}

	resp, err := req.Get(strings.TrimRight(apiBase, "/") + vllmModelsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get models from vllm: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("vllm returned status code %d: %s", resp.StatusCode(), resp.String())
	}

	var modelsResponse VLLMModelsResponse
	if err := json.Unmarshal(resp.Body(), &modelsResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vllm response: %w", err)
	}

	return &modelsResponse, nil
}
