package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
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
	httpClient *resty.Client
}

// NewLiteLLMClient creates a new LiteLLMClient. The base URL and master key are resolved from the
// environment (LITELLM_ENDPOINT, LITELLM_MASTER_KEY) or the mounted secret file.
func NewLiteLLMClient() LiteLLMClient {
	baseURL := defaultLiteLLMBaseURL
	if envURL := os.Getenv("LITELLM_ENDPOINT"); envURL != "" {
		baseURL = envURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	var masterKey string
	if key := os.Getenv("LITELLM_MASTER_KEY"); key != "" {
		masterKey = key
	} else if keyBytes, err := os.ReadFile("/etc/secret/litellm-secret/master-key"); err == nil {
		masterKey = strings.TrimSpace(string(keyBytes))
	}

	httpClient := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(defaultLiteLLMTimeout).
		SetHeader("Content-Type", "application/json")
	if masterKey != "" {
		httpClient.SetAuthToken(masterKey)
	}

	return &litellmClient{httpClient: httpClient}
}

// doRequest sends the request and returns the raw response body and status code. A non-2xx status
// is not an error here; callers decide what each status means.
func (c *litellmClient) doRequest(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	req := c.httpClient.R().SetContext(ctx)
	if body != nil {
		req.SetBody(body)
	}

	resp, err := req.Execute(method, path)
	if err != nil {
		return nil, 0, fmt.Errorf("http request failed: %w", err)
	}

	return resp.Body(), resp.StatusCode(), nil
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
