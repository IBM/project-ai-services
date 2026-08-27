package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	// digitizeHTTPTimeout is the per-request timeout for calls to a Digitize pod.
	digitizeHTTPTimeout = 15 * time.Second
	// digitizeMaxRetries is the number of retries on failure (one retry = two total attempts).
	digitizeMaxRetries = 1
)

// digitizeUpdatePayload is the request body for PUT /v1/connectors/<connector_id>.
// Only the fields being updated are sent; the Digitize service performs a partial update.
type digitizeUpdatePayload struct {
	// ConnectionDetails holds the updated credential fields.
	ConnectionDetails map[string]any `json:"connection_details"`
}

// DigitizeClient is an HTTP client for Digitize pod API calls.
// TLS verification is skipped because Digitize services are deployed with
// cluster-internal self-signed certificates (nip.io / OpenShift routes).
type DigitizeClient struct {
	http *resty.Client
}

// NewDigitizeClient creates a DigitizeClient pointed at baseURL.
// The resty client is configured with a 15-second timeout and TLS verification
// skipped for internal cluster communications.
// TODO : set the Insecure flag to conditionally based on self-signed certificates used.
func NewDigitizeClient(baseURL string) *DigitizeClient {
	r := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(digitizeHTTPTimeout).
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // internal service-to-service call with self-signed cert

	return &DigitizeClient{http: r}
}

// UpdateConnector calls PUT /v1/connectors/{connectorID} on the Digitize pod to propagate
// updated credentials. Retries once on failure. Returns nil on success.
func (c *DigitizeClient) UpdateConnector(ctx context.Context, connectorID string, updatedCreds map[string]any) error {
	payload := digitizeUpdatePayload{ConnectionDetails: updatedCreds}

	var lastErr error

	for attempt := 0; attempt <= digitizeMaxRetries; attempt++ {
		resp, err := c.http.R().
			SetContext(ctx).
			SetBody(payload).
			Put("/v1/connectors/" + connectorID)
		if err != nil {
			lastErr = fmt.Errorf("digitize PUT request failed: %w", err)

			continue
		}

		if resp.IsError() {
			lastErr = fmt.Errorf("digitize service returned unexpected status %d", resp.StatusCode())

			continue
		}

		return nil
	}

	return fmt.Errorf("failed to propagate credentials to Digitize service after %d attempt(s): %w", digitizeMaxRetries+1, lastErr)
}
