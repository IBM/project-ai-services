// Package httpproxy provides HTTP proxy primitives that are independent of any
// specific runtime. It mirrors the proxy package pattern:
//
//   - Response is the result type for any proxied HTTP call.
//   - HTTPProxier is the interface used by callers (e.g. ServiceClient).
//   - Exec makes a direct HTTP call via resty; used by the worker dispatcher
//     and by local runtimes (OpenShift, Podman) that have direct network access.
//   - RemoteHTTPProxier sends COMMAND_TYPE_HTTP_PROXY over the gRPC
//     CommandStream to a worker, which calls Exec locally and returns the result.
package httpproxy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-resty/resty/v2"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/payload"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// Response carries the result of a proxied HTTP request.
type Response struct {
	StatusCode int
	Headers    map[string]string
	Body       []byte
}

// HTTPProxier executes an HTTP request and returns the response.
// Exec implements this interface for direct (local) calls;
// RemoteHTTPProxier implements it for gRPC-tunnelled calls.
type HTTPProxier interface {
	HTTPProxy(ctx context.Context, method, targetURL string, headers map[string]string, body []byte) (*Response, error)
}

// Exec makes a direct HTTP request using resty and returns the response.
// Used by the worker dispatcher and by local runtimes that have direct network
// access to the target URL.
func Exec(ctx context.Context, method, targetURL string, headers map[string]string, body []byte) (*Response, error) {
	client := resty.New()

	req := client.R().SetContext(ctx)
	for k, v := range headers {
		req.SetHeader(k, v)
	}
	if len(body) > 0 {
		req.SetBody(body)
	}

	resp, err := req.Execute(method, targetURL)
	if err != nil {
		return nil, fmt.Errorf("httpproxy: execute request: %w", err)
	}

	respHeaders := make(map[string]string, len(resp.Header()))
	for k := range resp.Header() {
		respHeaders[k] = resp.Header().Get(k)
	}

	return &Response{
		StatusCode: resp.StatusCode(),
		Headers:    respHeaders,
		Body:       resp.Body(),
	}, nil
}

// RemoteHTTPProxier implements HTTPProxier by forwarding the request as a
// COMMAND_TYPE_HTTP_PROXY command over the gRPC CommandStream to a worker node,
// which calls Exec locally and returns the response.
type RemoteHTTPProxier struct {
	sender *stream.Sender
}

// NewRemoteHTTPProxier returns a RemoteHTTPProxier targeting the named worker.
func NewRemoteHTTPProxier(workerName string, reg stream.WorkerRegistry) *RemoteHTTPProxier {
	return &RemoteHTTPProxier{sender: stream.New(workerName, reg)}
}

// HTTPProxy implements HTTPProxier.
func (r *RemoteHTTPProxier) HTTPProxy(ctx context.Context, method, targetURL string, headers map[string]string, body []byte) (*Response, error) {
	res, err := r.sender.Send(ctx, workerpb.CommandType_COMMAND_TYPE_HTTP_PROXY,
		payload.HTTPProxy{
			Method:    method,
			TargetURL: targetURL,
			Headers:   headers,
			Body:      body,
		})
	if err != nil {
		return nil, err
	}

	var result Response
	if len(res.GetData()) > 0 {
		if err := json.Unmarshal(res.GetData(), &result); err != nil {
			return nil, fmt.Errorf("httpproxy: unmarshal response: %w", err)
		}
	}

	return &result, nil
}
