package e2e

// mcputil_test.go — shared helpers for MCP protocol HTTP calls used by both
// the standalone tests (mcp_health_test.go) and the Ginkgo suite (e2e_suite_test.go).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/tests/e2e/common"
)

const mcpRequestTimeout = 30 * time.Second //nolint:mnd

// mcpHTTPClient returns an HTTP client that skips TLS verification, suitable
// for test environments that use self-signed certificates.
func mcpHTTPClient() *http.Client {
	return common.GetHTTPClient(common.HTTPClientConfig{
		Timeout:            mcpRequestTimeout,
		InsecureSkipVerify: true,
	})
}

// mcpDoRequest sends a single MCP JSON-RPC POST to endpoint and returns the
// raw response body bytes and the Mcp-Session-Id header value.
func mcpDoRequest(ctx context.Context, client *http.Client, endpoint, body, sessionID string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("POST %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("POST %s returned HTTP %d", endpoint, resp.StatusCode)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read response: %w", err)
	}

	return raw, resp.Header.Get("Mcp-Session-Id"), nil
}

// mcpToolsForEndpoint performs the MCP initialize + tools/list handshake
// against endpoint (the /mcp URL) and returns the parsed tool list.
// Each tool is a map with at least "name", "description", and "inputSchema" keys.
func mcpToolsForEndpoint(endpoint string) ([]map[string]any, error) {
	client := mcpHTTPClient()

	ctx, cancel := context.WithTimeout(context.Background(), mcpRequestTimeout)
	defer cancel()

	// Step 1 — initialize.
	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"e2e-test","version":"1.0"}}}`
	_, sessionID, err := mcpDoRequest(ctx, client, endpoint, initBody, "")
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	// Step 2 — tools/list.
	listBody := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	raw, _, err := mcpDoRequest(ctx, client, endpoint, listBody, sessionID)
	if err != nil {
		return nil, fmt.Errorf("tools/list: %w", err)
	}

	// The server may respond with SSE format; extract the JSON payload.
	payload := sseExtractData(raw)

	var envelope struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("decode tools/list: %w (body: %s)", err, raw)
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("tools/list error %d: %s", envelope.Error.Code, envelope.Error.Message)
	}

	return envelope.Result.Tools, nil
}

// sseExtractData extracts the JSON payload from an SSE-formatted body.
// SSE format: "event: message\ndata: <json>\n\n"
// If the body is already plain JSON it is returned unchanged.
func sseExtractData(body []byte) []byte {
	s := string(body)
	if !strings.HasPrefix(strings.TrimSpace(s), "event:") {
		return body
	}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			return []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}

	return body
}
