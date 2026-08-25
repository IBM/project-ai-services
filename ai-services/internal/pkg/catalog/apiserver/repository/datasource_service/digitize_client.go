package datasourceservice

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	digitizeHTTPTimeout = 5 * time.Second
)

// digitizeClient is a dedicated HTTP client for Digitize calls.
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

// digitizeSyncResponse models the subset of GET /v1/connectors/{id} we need.
type digitizeSyncResponse struct {
	SyncStatus string  `json:"sync_status"`
	LastSyncAt *string `json:"last_sync_at"`
}

// fetchConnectorSyncFromDigitize calls GET /v1/connectors/{connectorID} on the Digitize pod
// at baseURL and returns the sync_status and last_sync_at values.
// Returns an error when the HTTP call fails or returns a non-200 status.
func fetchConnectorSyncFromDigitize(ctx context.Context, baseURL, connectorID string) (syncStatus string, lastSyncAt *string, err error) {
	url := strings.TrimRight(baseURL, "/") + "/v1/connectors/" + connectorID

	callCtx, cancel := context.WithTimeout(ctx, digitizeHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to build digitize request: %w", err)
	}

	resp, err := digitizeClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("digitize request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Drain body to allow connection reuse.
		_, _ = io.Copy(io.Discard, resp.Body)

		return "", nil, fmt.Errorf("digitize returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read digitize response: %w", err)
	}

	var result digitizeSyncResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", nil, fmt.Errorf("failed to parse digitize response: %w", err)
	}

	return result.SyncStatus, result.LastSyncAt, nil
}

// Made with Bob
