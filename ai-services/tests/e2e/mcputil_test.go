package e2e

// mcputil_test.go — shared helpers for MCP protocol HTTP calls used by both
// the standalone tests (mcp_health_test.go) and the Ginkgo suite (e2e_suite_test.go).

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	catalogConfig "github.com/project-ai-services/ai-services/internal/pkg/catalog/config"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
)

// mcpCallTool opens a fresh MCP session on endpoint, then invokes the named
// tool with the given params map via a tools/call request.
// The MCP sidecars run with --auth-passthrough, so every tools/call must carry
// the catalog bearer token in the Authorization header.
// The raw SSE-unwrapped JSON-RPC response payload is returned so callers can
// unmarshal the result.content array themselves.
func mcpCallTool(ctx context.Context, endpoint, toolName string, params map[string]any) ([]byte, error) {
	client := mcpToolCallHTTPClient()

	token, err := mcpBearerToken()
	if err != nil {
		return nil, err
	}

	// Step 1 — initialize, capture session ID.
	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","clientInfo":{"name":"e2e-test","version":"1.0"}}}`
	_, sessionID, err := mcpDoRequest(ctx, client, endpoint, initBody, "", token)
	if err != nil {
		return nil, fmt.Errorf("mcpCallTool initialize: %w", err)
	}

	// Step 2 — serialize params and issue tools/call.
	callArgs, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("mcpCallTool marshal params: %w", err)
	}
	callBody := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":%q,"arguments":%s}}`,
		toolName, callArgs,
	)
	raw, _, err := mcpDoRequest(ctx, client, endpoint, callBody, sessionID, token)
	if err != nil {
		return nil, fmt.Errorf("mcpCallTool tools/call %s: %w", toolName, err)
	}

	return sseExtractData(raw), nil
}

// mcpBearerToken returns the catalog access token saved by 'catalog login'.
func mcpBearerToken() (string, error) {
	creds, err := catalogConfig.Load()
	if err != nil {
		return "", fmt.Errorf("load catalog credentials: %w", err)
	}
	if creds.AccessToken == "" {
		return "", fmt.Errorf("catalog access token is empty — run catalog login first")
	}

	return creds.AccessToken, nil
}

// mcpToolResult is the JSON-RPC envelope returned by tools/call.
type mcpToolResult struct {
	Result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// mcpParseToolText decodes a tools/call response and returns content[0].text.
// It returns an error for JSON-RPC errors, tool errors and empty content.
func mcpParseToolText(raw []byte) (string, error) {
	var env mcpToolResult
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("decode tools/call response: %w (body: %s)", err, raw)
	}
	if env.Error != nil {
		return "", fmt.Errorf("tools/call error %d: %s", env.Error.Code, env.Error.Message)
	}
	if len(env.Result.Content) == 0 {
		return "", fmt.Errorf("tools/call result.content is empty (body: %s)", raw)
	}
	if env.Result.IsError {
		return "", fmt.Errorf("tool returned isError: %s", env.Result.Content[0].Text)
	}

	return env.Result.Content[0].Text, nil
}

// mcpUpstreamError reports whether text is a service error envelope of the
// form {"error":{...}} or {"detail":...}. The sidecar returns the upstream body
// as text without checking the HTTP status, so a 4xx/5xx looks like success.
func mcpUpstreamError(text string) error {
	var probe struct {
		Error  json.RawMessage `json:"error"`
		Detail json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal([]byte(text), &probe); err != nil {
		return nil
	}
	if len(probe.Error) > 0 && string(probe.Error) != "null" {
		return fmt.Errorf("upstream service returned an error: %s", text)
	}
	if len(probe.Detail) > 0 && string(probe.Detail) != "null" {
		return fmt.Errorf("upstream service returned an error: %s", text)
	}

	return nil
}

// mcpBase64File returns the standard base64 encoding of b, ready to embed as a
// file entry in the "files" array of the create_digitize_job prototype body.
func mcpBase64File(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

// mcpEnsureEndpoint appends /mcp to url if it does not already end with /mcp.
func mcpEnsureEndpoint(url string) string {
	if strings.HasSuffix(url, "/mcp") {
		return url
	}

	return strings.TrimSuffix(url, "/") + "/mcp"
}

const mcpRequestTimeout = 30 * time.Second //nolint:mnd

// mcpToolCallTimeout bounds a single tools/call HTTP round-trip. It is much
// longer than mcpRequestTimeout because chat completions and PDF uploads are slow.
const mcpToolCallTimeout = 10 * time.Minute //nolint:mnd

// mcpHTTPClient returns an HTTP client that skips TLS verification, suitable
// for test environments that use self-signed certificates.
func mcpHTTPClient() *http.Client {
	return common.GetHTTPClient(common.HTTPClientConfig{
		Timeout:            mcpRequestTimeout,
		InsecureSkipVerify: true,
	})
}

// mcpToolCallHTTPClient is like mcpHTTPClient but with a timeout long enough
// for tools/call requests that proxy to slow upstream services.
func mcpToolCallHTTPClient() *http.Client {
	return common.GetHTTPClient(common.HTTPClientConfig{
		Timeout:            mcpToolCallTimeout,
		InsecureSkipVerify: true,
	})
}

// mcpDoRequest sends a single MCP JSON-RPC POST to endpoint and returns the
// raw response body bytes and the Mcp-Session-Id header value.
// A non-empty bearerToken is sent as the Authorization header.
func mcpDoRequest(ctx context.Context, client *http.Client, endpoint, body, sessionID, bearerToken string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
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
	_, sessionID, err := mcpDoRequest(ctx, client, endpoint, initBody, "", "")
	if err != nil {
		return nil, fmt.Errorf("initialize: %w", err)
	}

	// Step 2 — tools/list.
	listBody := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	raw, _, err := mcpDoRequest(ctx, client, endpoint, listBody, sessionID, "")
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
