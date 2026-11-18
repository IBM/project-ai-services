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
	RegisterModel(ctx context.Context, req LiteLLMModelNewRequest) (string, error)
	DeregisterModel(ctx context.Context, modelID string) error
	RegisterPassThrough(ctx context.Context, req LiteLLMPassThroughRequest) error
	DeregisterPassThrough(ctx context.Context, endpointID string) error
	HealthProbe(ctx context.Context, modelID string) (*LiteLLMHealthResponse, error)
	// DeleteKeysByAlias revokes the virtual keys with the given aliases. An alias that has no key
	// is not an error.
	DeleteKeysByAlias(ctx context.Context, aliases []string) error
	// GenerateKey creates a virtual key and returns its raw "sk-..." value. The value must never
	// be logged or stored.
	GenerateKey(ctx context.Context, req LiteLLMKeyGenerateRequest) (string, error)
}

// LiteLLMKeyGenerateRequest is the payload for POST /key/generate. No duration is sent, so the
// key never expires.
type LiteLLMKeyGenerateRequest struct {
	KeyAlias string             `json:"key_alias"`
	Models   []string           `json:"models"`
	Metadata LiteLLMKeyMetadata `json:"metadata"`
}

// LiteLLMKeyMetadata is the metadata of a virtual key. Tags are the deployment IDs the key may
// reach: with tag filtering enabled, a request made with the key only goes to routes whose
// litellm_params.tags contain one of them.
type LiteLLMKeyMetadata struct {
	Tags []string `json:"tags"`
}

// LiteLLMKeyGenerateResponse is the response payload from POST /key/generate.
type LiteLLMKeyGenerateResponse struct {
	Key string `json:"key"`
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

// LiteLLMModelNewResponse is the response payload from POST /model/new.
type LiteLLMModelNewResponse struct {
	ModelID string `json:"model_id"`
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

func (c *litellmClient) RegisterModel(ctx context.Context, req LiteLLMModelNewRequest) (string, error) {
	respBytes, statusCode, err := c.doRequest(ctx, http.MethodPost, "/model/new", req)
	if err != nil {
		return "", fmt.Errorf("failed to register route with litellm: %w", err)
	}
	if statusCode < 200 || statusCode >= 300 {
		return "", fmt.Errorf("litellm /model/new returned status %d: %s", statusCode, string(respBytes))
	}

	var resp LiteLLMModelNewResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return "", fmt.Errorf("failed to parse litellm /model/new response: %w", err)
	}
	if resp.ModelID == "" {
		return "", fmt.Errorf("litellm /model/new response did not contain a model_id: %s", string(respBytes))
	}

	return resp.ModelID, nil
}

func (c *litellmClient) DeregisterModel(ctx context.Context, modelID string) error {
	body := map[string]string{"id": modelID}
	respBytes, statusCode, err := c.doRequest(ctx, http.MethodPost, "/model/delete", body)
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

func (c *litellmClient) DeregisterPassThrough(ctx context.Context, endpointID string) error {
	path := fmt.Sprintf("/config/pass_through_endpoint?endpoint_id=%s", url.QueryEscape(endpointID))
	respBytes, statusCode, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("failed to delete passthrough endpoint from litellm: %w", err)
	}
	if statusCode < 200 || (statusCode >= 300 && statusCode != http.StatusNotFound) {
		return fmt.Errorf("litellm DELETE /config/pass_through_endpoint returned status %d: %s", statusCode, string(respBytes))
	}

	return nil
}

func (c *litellmClient) DeleteKeysByAlias(ctx context.Context, aliases []string) error {
	body := map[string][]string{"key_aliases": aliases}
	respBytes, statusCode, err := c.doRequest(ctx, http.MethodPost, "/key/delete", body)
	if err != nil {
		return fmt.Errorf("failed to delete keys from litellm: %w", err)
	}
	if statusCode >= 200 && statusCode < 300 {
		return nil
	}
	// LiteLLM reports an alias without a key as 404, or as a 4xx/5xx whose message says not found.
	if statusCode == http.StatusNotFound || strings.Contains(strings.ToLower(string(respBytes)), "not found") {
		return nil
	}

	return fmt.Errorf("litellm /key/delete returned status %d: %s", statusCode, string(respBytes))
}

// GenerateKey never includes the response body in an error or log: on success it holds the key.
func (c *litellmClient) GenerateKey(ctx context.Context, req LiteLLMKeyGenerateRequest) (string, error) {
	respBytes, statusCode, err := c.doRequest(ctx, http.MethodPost, "/key/generate", req)
	if err != nil {
		return "", fmt.Errorf("failed to generate key in litellm: %w", err)
	}
	if statusCode < 200 || statusCode >= 300 {
		return "", fmt.Errorf("litellm /key/generate returned status %d", statusCode)
	}

	var resp LiteLLMKeyGenerateResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return "", fmt.Errorf("failed to parse litellm /key/generate response: %w", err)
	}
	if resp.Key == "" {
		return "", fmt.Errorf("litellm /key/generate response did not contain a key")
	}

	return resp.Key, nil
}

func (c *litellmClient) HealthProbe(ctx context.Context, modelID string) (*LiteLLMHealthResponse, error) {
	path := fmt.Sprintf("/health?model_id=%s", url.QueryEscape(modelID))
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
