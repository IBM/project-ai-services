package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

const (
	// serviceMaxRetries is the number of retries on failure (one retry = two total attempts).
	serviceMaxRetries  = 1
	serviceConnectPath = "/v1/connectors"
)

// serviceConnectorListResponse is the paginated envelope returned by
// GET /v1/connectors on the downstream service pod.
type serviceConnectorListResponse struct {
	Total  int                       `json:"total"`
	Limit  int                       `json:"limit"`
	Offset int                       `json:"offset"`
	Items  []apimodels.ConnectorItem `json:"items"`
}

// ServiceConnectorPage is the parsed result of a ListConnectors call: the page items keyed by
// connector ID for O(1) lookup, plus the Total count reported by the service for pagination.
type ServiceConnectorPage struct {
	ByID  map[string]apimodels.ConnectorItem
	Total int
}

// ServiceHTTPError is returned by client methods when the downstream service responds
// with a non-2xx status code. Callers can type-assert to inspect the status code and
// decide whether to treat specific codes (e.g. 404) as non-fatal.
type ServiceHTTPError struct {
	StatusCode int
}

func (e *ServiceHTTPError) Error() string {
	return fmt.Sprintf("service returned status %d", e.StatusCode)
}

// serviceUpdatePayload is the request body for PUT /v1/connectors/<connector_id>.
type serviceUpdatePayload struct {
	ConnectionDetails map[string]any `json:"connection_details"`
}

// ServiceClient calls downstream service pod endpoints (e.g. Digitize) via
// runtime.HTTPProxy — all HTTP traffic is tunnelled through the worker so that
// internal pod URLs (svc.cluster.local or Podman container names) are reachable
// from the control plane.
type ServiceClient struct {
	rt      runtime.Runtime
	baseURL string
}

// NewServiceClient creates a ServiceClient that routes all calls through rt.HTTPProxy
// to the given baseURL.
func NewServiceClient(rt runtime.Runtime, baseURL string) *ServiceClient {
	return &ServiceClient{rt: rt, baseURL: baseURL}
}

// do executes a single HTTP request via HTTPProxy and returns the raw response.
func (c *ServiceClient) do(ctx context.Context, method, path string, body []byte) (statusCode int, respBody []byte, err error) {
	headers := map[string]string(nil)
	if len(body) > 0 {
		headers = map[string]string{"Content-Type": "application/json"}
	}

	resp, err := c.rt.HTTPProxy(ctx, method, c.baseURL+path, headers, body)
	if err != nil {
		return 0, nil, err
	}

	return resp.StatusCode, resp.Body, nil
}

// UpdateConnector calls PUT /v1/connectors/{connectorID} to propagate updated credentials.
// Retries once on failure.
func (c *ServiceClient) UpdateConnector(ctx context.Context, connectorID string, updatedCreds map[string]any) error {
	body, err := json.Marshal(serviceUpdatePayload{ConnectionDetails: updatedCreds})
	if err != nil {
		return fmt.Errorf("marshal update payload: %w", err)
	}

	var lastErr error

	for attempt := 0; attempt <= serviceMaxRetries; attempt++ {
		status, _, reqErr := c.do(ctx, http.MethodPut, serviceConnectPath+"/"+connectorID, body)
		if reqErr != nil {
			lastErr = fmt.Errorf("service PUT request failed: %w", reqErr)
			continue
		}

		if status < 200 || status >= 300 {
			lastErr = fmt.Errorf("service returned unexpected status %d", status)
			continue
		}

		return nil
	}

	return fmt.Errorf("failed to propagate credentials after %d attempt(s): %w", serviceMaxRetries+1, lastErr)
}

// GetConnectorSync calls GET /v1/connectors/{connectorID} and returns the connector's
// sync_status and related fields. Returns an error on HTTP failure or non-200 response.
func (c *ServiceClient) GetConnectorSync(ctx context.Context, connectorID string) (*apimodels.ConnectorSyncState, error) {
	status, body, err := c.do(ctx, http.MethodGet, serviceConnectPath+"/"+connectorID, nil)
	if err != nil {
		return nil, fmt.Errorf("service GET request failed: %w", err)
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("service returned status %d", status)
	}

	var result apimodels.ConnectorSyncState
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode sync state: %w", err)
	}

	return &result, nil
}

// ListConnectors calls GET /v1/connectors with limit/offset pagination and returns a
// ServiceConnectorPage keyed by connector ID for O(1) lookup. Pass limit=0 to use the
// service default. Returns an error on HTTP failure or non-200 response.
func (c *ServiceClient) ListConnectors(ctx context.Context, limit, offset int) (*ServiceConnectorPage, error) {
	path := serviceConnectPath
	if limit > 0 || offset > 0 {
		path += "?limit=" + strconv.Itoa(limit) + "&offset=" + strconv.Itoa(offset)
	}

	status, body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("service GET request failed: %w", err)
	}

	if status != http.StatusOK {
		return nil, fmt.Errorf("service returned status %d", status)
	}

	var result serviceConnectorListResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode connector list: %w", err)
	}

	byID := make(map[string]apimodels.ConnectorItem, len(result.Items))
	for _, item := range result.Items {
		byID[item.ID] = item
	}

	return &ServiceConnectorPage{ByID: byID, Total: result.Total}, nil
}

// Connect calls POST /v1/connectors on the service pod.
// 409 Conflict is treated as success — connector already exists (idempotent).
func (c *ServiceClient) Connect(ctx context.Context, req apimodels.ConnectDatasourceRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal connect request: %w", err)
	}

	status, _, err := c.do(ctx, http.MethodPost, serviceConnectPath, body)
	if err != nil {
		return fmt.Errorf("service POST request failed: %w", err)
	}

	if status == http.StatusConflict {
		return nil
	}

	if status < 200 || status >= 300 {
		return fmt.Errorf("service returned unexpected status %d", status)
	}

	return nil
}

// Disconnect calls DELETE /v1/connectors/{connectorID} on the service pod.
// Returns a *ServiceHTTPError so callers can inspect the status code (e.g. treat 404 as success).
func (c *ServiceClient) Disconnect(ctx context.Context, connectorID string) error {
	status, _, err := c.do(ctx, http.MethodDelete, serviceConnectPath+"/"+connectorID, nil)
	if err != nil {
		return fmt.Errorf("service DELETE request failed: %w", err)
	}

	if status < 200 || status >= 300 {
		return &ServiceHTTPError{StatusCode: status}
	}

	return nil
}
