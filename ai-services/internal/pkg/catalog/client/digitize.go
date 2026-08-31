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
