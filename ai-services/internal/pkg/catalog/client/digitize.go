package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
)

const (
	// digitizeHTTPTimeout is the per-request timeout for calls to a Digitize pod.
	digitizeHTTPTimeout = 5 * time.Second
)

// DigitizeClient is an HTTP client for Digitize pod API calls.
// TLS verification is skipped because Digitize services are deployed with
// cluster-internal self-signed certificates (nip.io / OpenShift routes).
type DigitizeClient struct {
	http *resty.Client
}

// NewDigitizeClient creates a DigitizeClient pointed at baseURL.
// The resty client is configured with a 5-second timeout and TLS verification
// skipped for internal cluster communications.
// TODO : set the Insecure flag to conditionally based on self-signed certificates used.
func NewDigitizeClient(baseURL string) *DigitizeClient {
	r := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(digitizeHTTPTimeout).
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // internal service-to-service call with self-signed cert

	return &DigitizeClient{http: r}
}

// GetConnectorSync calls GET /v1/connectors/{connectorID} on the Digitize pod and
// returns the connector's sync_status and last_sync_at values.
// Returns an error when the HTTP call fails or the pod returns a non-200 status.
func (c *DigitizeClient) GetConnectorSync(ctx context.Context, connectorID string) (*apimodels.ConnectorSyncState, error) {
	var result apimodels.ConnectorSyncState

	resp, err := c.http.R().
		SetContext(ctx).
		SetResult(&result).
		Get("/v1/connectors/" + connectorID)
	if err != nil {
		return nil, fmt.Errorf("digitize request failed: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("digitize returned status %d", resp.StatusCode())
	}

	return &result, nil
}

// ListConnectors calls GET /v1/connectors on the Digitize pod and returns all connectors
// as a map keyed by connector ID for O(1) lookup.
// The list endpoint is not paginated — it always returns all connectors on the pod.
func (c *DigitizeClient) ListConnectors(ctx context.Context) (map[string]apimodels.DigitizeConnectorItem, error) {
	var result []apimodels.DigitizeConnectorItem

	resp, err := c.http.R().
		SetContext(ctx).
		SetResult(&result).
		Get("/v1/connectors")
	if err != nil {
		return nil, fmt.Errorf("digitize request failed: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("digitize returned status %d", resp.StatusCode())
	}

	byID := make(map[string]apimodels.DigitizeConnectorItem, len(result))
	for _, item := range result {
		byID[item.ID] = item
	}

	return byID, nil
}

// GetLatestConnectorSyncLog calls GET /v1/connectors/{connectorID}/syncs?latest=true on the
// Digitize pod and returns the most recent sync log entry.
// The latest=true query parameter will be implemented on the Digitize side; the caller
// uses the first item in the returned list.
// Returns nil (no error) when no sync log exists yet for the connector.
func (c *DigitizeClient) GetLatestConnectorSyncLog(ctx context.Context, connectorID string) (*apimodels.ConnectorSyncLog, error) {
	var result apimodels.ConnectorSyncLogResponse

	resp, err := c.http.R().
		SetContext(ctx).
		SetQueryParam("latest", "true").
		SetResult(&result).
		Get("/v1/connectors/" + connectorID + "/syncs")
	if err != nil {
		return nil, fmt.Errorf("digitize request failed: %w", err)
	}

	if resp.IsError() {
		return nil, fmt.Errorf("digitize returned status %d", resp.StatusCode())
	}

	if len(result.Items) == 0 {
		return nil, nil
	}

	return &result.Items[0], nil
}
