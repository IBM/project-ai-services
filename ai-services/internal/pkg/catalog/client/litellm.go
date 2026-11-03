package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultLiteLLMBaseURL = "http://localhost:4000"
	defaultLiteLLMTimeout = 15 * time.Second
)

// LiteLLMClient defines the client interface for LiteLLM Gateway operations.
type LiteLLMClient interface {
	RegisterModel(ctx context.Context, req LiteLLMModelNewRequest) error
	DeregisterModel(ctx context.Context, modelName string) error
	RegisterPassThrough(ctx context.Context, req LiteLLMPassThroughRequest) error
	DeregisterPassThrough(ctx context.Context, id string) error
	HealthProbe(ctx context.Context, modelName string) (*LiteLLMHealthResponse, error)
}

// LiteLLMModelInfo holds routing mode in /model/new (embedding/rerank).
type LiteLLMModelInfo struct {
	Mode string `json:"mode,omitempty"`
}

// LiteLLMModelNewRequest is the payload for POST /model/new.
//
// LiteLLMParams is the free-form litellm_params object. It is a map rather than a struct because
// the set of provider params (api_base, api_key, project_id, ...) is defined by each provider's
// schema.json and is forwarded as is; the client must not need a change when a schema gains a field.
type LiteLLMModelNewRequest struct {
	ModelName     string            `json:"model_name"`
	LiteLLMParams map[string]any    `json:"litellm_params"`
	ModelInfo     *LiteLLMModelInfo `json:"model_info,omitempty"`
}

// LiteLLMPassThroughRequest is the payload for POST /config/pass_through_endpoint.
type LiteLLMPassThroughRequest struct {
	ID      string   `json:"id"`
	Path    string   `json:"path"`
	Target  string   `json:"target"`
	Methods []string `json:"methods"`
	Auth    bool     `json:"auth"`
}

// LiteLLMHealthEndpoint represents endpoint details in LiteLLM health probe response.
type LiteLLMHealthEndpoint struct {
	APIBase         string `json:"api_base"`
	Model           string `json:"model"`
	ModelID         string `json:"model_id"`
	Error           string `json:"error,omitempty"`
	ExceptionStatus int    `json:"exception_status,omitempty"`
}

// LiteLLMHealthResponse is the response from GET /health?model=...
type LiteLLMHealthResponse struct {
	HealthyEndpoints   []LiteLLMHealthEndpoint `json:"healthy_endpoints"`
	UnhealthyEndpoints []LiteLLMHealthEndpoint `json:"unhealthy_endpoints"`
	HealthyCount       int                     `json:"healthy_count"`
	UnhealthyCount     int                     `json:"unhealthy_count"`
}

type litellmClient struct {
	baseURL    string
	masterKey  string
	httpClient *http.Client
}

// NewLiteLLMClient creates a new LiteLLMClient.
func NewLiteLLMClient(baseURL, masterKey string) LiteLLMClient {
	if baseURL == "" {
		if envURL := os.Getenv("LITELLM_ENDPOINT"); envURL != "" {
			baseURL = envURL
		} else if envURL := os.Getenv("LITELLM_URL"); envURL != "" {
			baseURL = envURL
		} else {
			baseURL = defaultLiteLLMBaseURL
		}
	}
	baseURL = strings.TrimRight(baseURL, "/")

	if masterKey == "" {
		if key := os.Getenv("LITELLM_MASTER_KEY"); key != "" {
			masterKey = key
		} else if keyBytes, err := os.ReadFile("/etc/secret/litellm-secret/master-key"); err == nil {
			masterKey = strings.TrimSpace(string(keyBytes))
		}
	}

	return &litellmClient{
		baseURL:   baseURL,
		masterKey: masterKey,
		httpClient: &http.Client{
			Timeout: defaultLiteLLMTimeout,
		},
	}
}

func (c *litellmClient) doRequest(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBytes)
	}

	reqURL := fmt.Sprintf("%s%s", c.baseURL, path)
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create http request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.masterKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.masterKey))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("failed to read response body: %w", err)
	}

	return respBytes, resp.StatusCode, nil
}

func (c *litellmClient) RegisterModel(ctx context.Context, req LiteLLMModelNewRequest) error {
	respBytes, statusCode, err := c.doRequest(ctx, http.MethodPost, "/model/new", req)
	if err != nil {
		return fmt.Errorf("failed to register route with litellm: %w", err)
	}
	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("litellm /model/new returned status %d: %s", statusCode, string(respBytes))
	}

	return nil
}

func (c *litellmClient) DeregisterModel(ctx context.Context, modelName string) error {
	body := map[string]string{"model_name": modelName}
	respBytes, statusCode, err := c.doRequest(ctx, http.MethodDelete, "/model/delete", body)
	if err != nil {
		return fmt.Errorf("failed to delete route from litellm: %w", err)
	}
	if statusCode < 200 || statusCode >= 300 && statusCode != http.StatusNotFound {
		return fmt.Errorf("litellm /model/delete returned status %d: %s", statusCode, string(respBytes))
	}

	return nil
}

func (c *litellmClient) RegisterPassThrough(ctx context.Context, req LiteLLMPassThroughRequest) error {
	respBytes, statusCode, err := c.doRequest(ctx, http.MethodPost, "/config/pass_through_endpoint", req)
	if err != nil {
		return fmt.Errorf("failed to register passthrough endpoint with litellm: %w", err)
	}
	if statusCode < 200 || statusCode >= 300 {
		return fmt.Errorf("litellm /config/pass_through_endpoint returned status %d: %s", statusCode, string(respBytes))
	}

	return nil
}

func (c *litellmClient) DeregisterPassThrough(ctx context.Context, id string) error {
	body := map[string]string{"id": id}
	respBytes, statusCode, err := c.doRequest(ctx, http.MethodDelete, "/config/pass_through_endpoint", body)
	if err != nil {
		return fmt.Errorf("failed to delete passthrough endpoint from litellm: %w", err)
	}
	if statusCode < 200 || statusCode >= 300 && statusCode != http.StatusNotFound {
		return fmt.Errorf("litellm DELETE /config/pass_through_endpoint returned status %d: %s", statusCode, string(respBytes))
	}

	return nil
}

func (c *litellmClient) HealthProbe(ctx context.Context, modelName string) (*LiteLLMHealthResponse, error) {
	path := fmt.Sprintf("/health?model=%s", url.QueryEscape(modelName))
	respBytes, statusCode, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("health probe request failed: %w", err)
	}
	if statusCode < 200 || statusCode >= 300 {
		return nil, fmt.Errorf("litellm /health returned status %d: %s", statusCode, string(respBytes))
	}

	var healthResp LiteLLMHealthResponse
	if err := json.Unmarshal(respBytes, &healthResp); err != nil {
		return nil, fmt.Errorf("failed to parse litellm health probe response: %w", err)
	}

	return &healthResp, nil
}
