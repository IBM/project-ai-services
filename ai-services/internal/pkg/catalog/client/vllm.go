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

const defaultVLLMTimeout = 15 * time.Second

// VLLMModelInfo represents a model returned by vLLM.
type VLLMModelInfo struct {
	ID          string `json:"id"`
	MaxModelLen int    `json:"max_model_len"`
}

// VLLMModelsResponse is the response format for vLLM /v1/models.
type VLLMModelsResponse struct {
	Data []VLLMModelInfo `json:"data"`
}

// VLLMClient is a client for the vLLM API.
type VLLMClient struct {
	client *resty.Client
}

// NewVLLMClient creates a new vLLM client.
func NewVLLMClient(apiBase, apiKey string) *VLLMClient {
	baseURL := strings.TrimRight(apiBase, "/")

	rClient := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(defaultVLLMTimeout).
		SetHeader("Content-Type", "application/json")

	if apiKey != "" {
		rClient.SetAuthToken(apiKey)
	}

	return &VLLMClient{client: rClient}
}

// GetModels retrieves the list of models from vLLM.
func (c *VLLMClient) GetModels(ctx context.Context) (*VLLMModelsResponse, error) {
	resp, err := c.client.R().
		SetContext(ctx).
		Get("/v1/models")
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
