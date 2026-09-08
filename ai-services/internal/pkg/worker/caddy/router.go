// Package caddy provides the worker-side Caddy proxy router.
// It lives entirely outside the runtime package — Caddy management is a worker
// concern, not a core runtime concern.
package caddy

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/payload"
)

// ProxyRouter manages Caddy proxy routes on the local worker node.
// Construct it with New after the Caddy pod is running, then pass it to
// dispatch.Dispatch.
type ProxyRouter struct {
	pm proxy.ProxyManager
}

// NewProxyRouter builds a ProxyRouter pointed at the local Caddy admin API.
// CADDY_ADMIN_URL is injected into the worker pod at deploy time;
// GetCaddyProxyManager reads it directly.
func NewProxyRouter(ctx context.Context) (*ProxyRouter, error) {
	pm, err := proxy.GetCaddyProxyManager()
	if err != nil {
		return nil, fmt.Errorf("caddy router: %w", err)
	}

	return &ProxyRouter{pm: pm}, nil
}

// ManageProxyRoute dispatches a Caddy proxy operation to the local Caddy instance.
func (pr *ProxyRouter) ManageProxyRoute(ctx context.Context, op payload.ProxyRouteOp, route payload.Route) (*payload.Route, error) {
	r := proxy.Route{
		ID:       route.ID,
		Domain:   route.Domain,
		Upstream: route.Upstream,
		Terminal: route.Terminal,
		Type:     route.Type,
	}

	switch op {
	case payload.ProxyRouteOpRegister:
		// RegisterRoute returns the external URL built from the worker's own
		// CADDY_HTTPS_PORT env var — the correct port for this machine.
		externalURL, err := pr.pm.RegisterRoute(ctx, r)
		if err != nil {
			return nil, err
		}

		return &payload.Route{
			ID:          r.ID,
			Domain:      r.Domain,
			Upstream:    r.Upstream,
			Terminal:    r.Terminal,
			Type:        r.Type,
			ExternalURL: externalURL,
		}, nil

	case payload.ProxyRouteOpUnregister:
		return nil, pr.pm.UnregisterRoute(ctx, route.ID)

	case payload.ProxyRouteOpGet:
		got, err := pr.pm.GetRouteByID(ctx, route.ID)
		if err != nil {
			return nil, err
		}

		return &payload.Route{
			ID:       got.ID,
			Domain:   got.Domain,
			Upstream: got.Upstream,
			Terminal: got.Terminal,
			Type:     got.Type,
		}, nil

	case payload.ProxyRouteOpHealthCheck:
		return nil, pr.pm.HealthCheck(ctx)

	default:
		return nil, fmt.Errorf("caddy router: unsupported op %q", op)
	}
}
