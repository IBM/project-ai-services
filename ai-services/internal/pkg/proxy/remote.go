package proxy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/worker/payload"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/stream"
)

// RemoteProxyManager implements ProxyManager by sending COMMAND_TYPE_PROXY_ROUTE
// commands over the gRPC CommandStream to a remote worker. Use it in the
// deployer when the runtime is a RemoteRuntime so route registration flows
// through the same gRPC channel as all other runtime calls.
type RemoteProxyManager struct {
	sender *stream.Sender
}

// NewRemoteProxyManager returns a RemoteProxyManager that reuses the given
// Sender (and therefore the same worker connection) as the RemoteRuntime.
func NewRemoteProxyManager(sender *stream.Sender) *RemoteProxyManager {
	return &RemoteProxyManager{sender: sender}
}

// RegisterRoute implements ProxyManager. It forwards the registration to the
// remote worker and reads the ExternalURL back from the CommandResult — the
// worker builds it from its own DOMAIN_SUFFIX/CADDY_HTTPS_PORT env vars.
func (r *RemoteProxyManager) RegisterRoute(ctx context.Context, route Route) (string, error) {
	res, err := r.send(ctx, payload.ProxyRoute{
		Op:       payload.ProxyRouteOpRegister,
		ID:       route.ID,
		Domain:   route.Domain,
		Upstream: route.Upstream,
		Terminal: route.Terminal,
		Type:     route.Type,
	})
	if err != nil {
		return "", err
	}

	// Worker serialises the registered payload.Route (with ExternalURL) into Data.
	if len(res.GetData()) == 0 {
		return "", nil
	}

	var registered payload.Route
	if err := json.Unmarshal(res.GetData(), &registered); err != nil {
		return "", fmt.Errorf("remote proxy manager: unmarshal registered route: %w", err)
	}

	return registered.ExternalURL, nil
}

// UnregisterRoute implements ProxyManager.
func (r *RemoteProxyManager) UnregisterRoute(ctx context.Context, routeID string) error {
	_, err := r.send(ctx, payload.ProxyRoute{
		Op: payload.ProxyRouteOpUnregister,
		ID: routeID,
	})

	return err
}

// GetRouteByID implements ProxyManager.
func (r *RemoteProxyManager) GetRouteByID(ctx context.Context, routeID string) (*Route, error) {
	res, err := r.send(ctx, payload.ProxyRoute{
		Op: payload.ProxyRouteOpGet,
		ID: routeID,
	})
	if err != nil {
		return nil, err
	}

	if len(res.GetData()) == 0 {
		return nil, nil
	}

	var result Route
	if err := json.Unmarshal(res.GetData(), &result); err != nil {
		return nil, fmt.Errorf("remote proxy manager: unmarshal route: %w", err)
	}

	return &result, nil
}

// HealthCheck implements ProxyManager.
func (r *RemoteProxyManager) HealthCheck(ctx context.Context) error {
	_, err := r.send(ctx, payload.ProxyRoute{Op: payload.ProxyRouteOpHealthCheck})

	return err
}

func (r *RemoteProxyManager) send(ctx context.Context, p payload.ProxyRoute) (*workerpb.CommandResult, error) {
	return r.sender.Send(ctx, workerpb.CommandType_COMMAND_TYPE_PROXY_ROUTE, p)
}
