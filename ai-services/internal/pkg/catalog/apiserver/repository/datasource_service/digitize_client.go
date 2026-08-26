package datasourceservice

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
)

const (
	digitizeConnectPath = "/v1/connectors"
	digitizeHTTPTimeout = 15 * time.Second
)

// digitizeClient is a dedicated HTTP client for Digitize propagation calls.
// TLS verification is skipped because Digitize services are deployed with
// cluster-internal self-signed certificates (nip.io / OpenShift routes).
// Using a named client (rather than http.DefaultClient) ensures Transport-level
// timeouts are set and the shared global is not inadvertently modified.
var digitizeClient = &http.Client{
	Timeout: digitizeHTTPTimeout,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // internal service-to-service call with self-signed cert
	},
}

// digitizeClientAdapter sends connector requests to downstream Digitize services.
type digitizeClientAdapter struct{}

// NewDigitizeClient returns a new DigitizeClientInterface backed by the shared HTTP client.
func NewDigitizeClient() DigitizeClientInterface {
	return &digitizeClientAdapter{}
}

// Connect calls POST /v1/connectors on the given Digitize base URL.
func (c *digitizeClientAdapter) Connect(ctx context.Context, baseURL string, req apimodels.ConnectDatasourceRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal digitize connect payload: %w", err)
	}

	return doDigitizePost(ctx, baseURL+digitizeConnectPath, body)
}

// doDigitizePost executes a single POST call to the digitize service.
func doDigitizePost(ctx context.Context, url string, body []byte) error {
	callCtx, cancel := context.WithTimeout(ctx, digitizeHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create digitize request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := digitizeClient.Do(req)
	if err != nil {
		return fmt.Errorf("digitize POST request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Drain and discard the body to allow connection reuse.
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusAccepted && (resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices) {
		return fmt.Errorf("digitize service returned unexpected status %d", resp.StatusCode)
	}

	return nil
}
