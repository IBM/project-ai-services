package vllm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const defaultTimeout = 15 * time.Second

// ModelInfo represents a model returned by vLLM.
type ModelInfo struct {
	ID          string `json:"id"`
	MaxModelLen int    `json:"max_model_len"`
}

// ModelsResponse is the response format for vLLM /v1/models.
type ModelsResponse struct {
	Data []ModelInfo `json:"data"`
}

// Client is a client for the vLLM API.
type Client struct {
	client *resty.Client
}

// NewClient creates a new vLLM client.
func NewClient(apiBase, apiKey string) *Client {
	baseURL := strings.TrimRight(apiBase, "/")

	rClient := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(defaultTimeout).
		SetHeader("Content-Type", "application/json")

	if apiKey != "" {
		rClient.SetAuthToken(apiKey)
	}

	return &Client{client: rClient}
}

// GetModels retrieves the list of models from vLLM.
func (c *Client) GetModels(ctx context.Context) (*ModelsResponse, error) {
	resp, err := c.client.R().
		SetContext(ctx).
		Get("/v1/models")
	if err != nil {
		return nil, fmt.Errorf("failed to get models from vllm: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("vllm returned status code %d: %s", resp.StatusCode(), resp.String())
	}

	var modelsResponse ModelsResponse
	if err := json.Unmarshal(resp.Body(), &modelsResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vllm response: %w", err)
	}

	return &modelsResponse, nil
}
