package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
)

// serviceConnectorListResponse is the paginated envelope returned by
// GET /v1/connectors on the downstream service pod.
type serviceConnectorListResponse struct {
	Total  int                       `json:"total"`
	Limit  int                       `json:"limit"`
	Offset int                       `json:"offset"`
	Items  []apimodels.ConnectorItem `json:"items"`
}

const (
	// serviceHTTPTimeout is the per-request timeout for calls to a downstream service pod.
	serviceHTTPTimeout = 15 * time.Second
	// serviceMaxRetries is the number of retries on failure (one retry = two total attempts).
	serviceMaxRetries  = 1
	serviceConnectPath = "/v1/connectors"
)

// serviceUpdatePayload is the request body for PUT /v1/connectors/<connector_id>.
// Only the fields being updated are sent; the service performs a partial update.
type serviceUpdatePayload struct {
	// ConnectionDetails holds the updated credential fields.
	ConnectionDetails map[string]any `json:"connection_details"`
}

// ServiceClient is an HTTP client for downstream service API calls (e.g. Digitize).
// TLS verification is skipped because services are deployed with
// cluster-internal self-signed certificates (nip.io / OpenShift routes).
type ServiceClient struct {
	http *resty.Client
}

// NewServiceClient creates a ServiceClient pointed at baseURL.
// The resty client is configured with a 15-second timeout and TLS verification
// skipped for internal cluster communications.
// TODO : set the Insecure flag to conditionally based on self-signed certificates used.
func NewServiceClient(baseURL string) *ServiceClient {
	r := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(serviceHTTPTimeout).
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // internal service-to-service call with self-signed cert

	return &ServiceClient{http: r}
}

// UpdateConnector calls PUT /v1/connectors/{connectorID} on the service pod to propagate
// updated credentials. Retries once on failure. Returns nil on success.
func (c *ServiceClient) UpdateConnector(ctx context.Context, connectorID string, updatedCreds map[string]any) error {
	payload := serviceUpdatePayload{ConnectionDetails: updatedCreds}

	var lastErr error

	for attempt := 0; attempt <= serviceMaxRetries; attempt++ {
		resp, err := c.http.R().
			SetContext(ctx).
			SetBody(payload).
			Put("/v1/connectors/" + connectorID)
		if err != nil {
			lastErr = fmt.Errorf("service PUT request failed: %w", err)

			continue
		}

		if resp.IsError() {
			lastErr = fmt.Errorf("service returned unexpected status %d", resp.StatusCode())

			continue
		}

		return nil
	}

	return fmt.Errorf("failed to propagate credentials to service after %d attempt(s): %w", serviceMaxRetries+1, lastErr)
}

// GetConnectorSync calls GET /v1/connectors/{connectorID} on the service pod and
// returns the connector's sync_status and last_sync_at values.
// Returns an error when the HTTP call fails or the pod returns a non-200 status.
func (c *ServiceClient) GetConnectorSync(ctx context.Context, connectorID string) (*apimodels.ConnectorSyncState, error) {
	var result apimodels.ConnectorSyncState

	resp, err := c.http.R().
		SetContext(ctx).
		SetResult(&result).
		Get("/v1/connectors/" + connectorID)
	if err != nil {
		return nil, fmt.Errorf("service request failed: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("service returned status %d", resp.StatusCode())
	}

	return &result, nil
}

// ListConnectors calls GET /v1/connectors on the service pod with limit/offset pagination
// and returns all connectors in the page as a map keyed by connector ID for O(1) lookup.
// The ConnectorListItem already includes the message field covering all status phases and
// error details — no separate sync-log call is required.
// Pass limit=0 to use the service default (50). offset is zero-based.
// Returns an error when the HTTP call fails or the pod returns a non-200 status.
func (c *ServiceClient) ListConnectors(ctx context.Context, limit, offset int) (map[string]apimodels.ConnectorItem, error) {
	var result serviceConnectorListResponse

	req := c.http.R().
		SetContext(ctx).
		SetResult(&result)

	if limit > 0 {
		req = req.SetQueryParam("limit", strconv.Itoa(limit))
	}

	if offset > 0 {
		req = req.SetQueryParam("offset", strconv.Itoa(offset))
	}

	resp, err := req.Get(serviceConnectPath)
	if err != nil {
		return nil, fmt.Errorf("service request failed: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("service returned status %d", resp.StatusCode())
	}

	byID := make(map[string]apimodels.ConnectorItem, len(result.Items))
	for _, item := range result.Items {
		byID[item.ID] = item
	}

	return byID, nil
}

// Connect calls POST /v1/connectors on the given service base URL.
// 409 Conflict is treated as success (idempotent — connector already exists).
func (c *ServiceClient) Connect(ctx context.Context, baseURL string, req apimodels.ConnectDatasourceRequest) error {
	resp, err := c.http.R().
		SetContext(ctx).
		SetBody(req).
		Post(baseURL + serviceConnectPath)
	if err != nil {
		return fmt.Errorf("service POST request failed: %w", err)
	}

	if resp.StatusCode() == http.StatusConflict {
		return nil
	}

	if resp.IsError() {
		return fmt.Errorf("service returned unexpected status %d", resp.StatusCode())
	}

	return nil
}
