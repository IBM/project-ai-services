package datasourceservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	digitizeUpdatePath  = "/v1/connectors/%s"
	digitizeHTTPTimeout = 15 * time.Second
	digitizeMaxRetries  = 1 // one retry on failure (two total attempts)
)

// digitizeClient is a dedicated HTTP client for Digitize propagation calls.
// Using a named client (rather than http.DefaultClient) ensures Transport-level
// timeouts are set and the shared global is not inadvertently modified.
var digitizeClient = &http.Client{Timeout: digitizeHTTPTimeout}

// digitizeUpdatePayload is the request body for PUT /v1/connectors/<connector_id>.
// Only the fields being updated are sent; the digitize service performs a partial update.
type digitizeUpdatePayload struct {
	// ConnectionDetails holds the updated credential fields.
	ConnectionDetails map[string]any `json:"connection_details"`
}

// updateDigitizeConnector calls PUT /v1/connectors/<connectorID> on the given base URL.
// It retries once on failure. Returns nil on success.
func updateDigitizeConnector(ctx context.Context, baseURL, connectorID string, updatedCreds map[string]any) error {
	payload := digitizeUpdatePayload{ConnectionDetails: updatedCreds}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal digitize update payload: %w", err)
	}

	url := fmt.Sprintf("%s"+digitizeUpdatePath, baseURL, connectorID)
	var lastErr error

	for attempt := 0; attempt <= digitizeMaxRetries; attempt++ {
		lastErr = doDigitizePut(ctx, url, body)
		if lastErr == nil {
			return nil
		}
	}

	return fmt.Errorf("failed to propagate credentials to Digitize service after %d attempt(s): %w", digitizeMaxRetries+1, lastErr)
}

// doDigitizePut executes a single PUT call to the digitize service.
func doDigitizePut(ctx context.Context, url string, body []byte) error {
	callCtx, cancel := context.WithTimeout(ctx, digitizeHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create digitize request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := digitizeClient.Do(req)
	if err != nil {
		return fmt.Errorf("digitize PUT request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Drain and discard the body to allow connection reuse.
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("digitize service returned unexpected status %d", resp.StatusCode)
	}

	return nil
}
