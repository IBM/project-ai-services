// Package caddy provides the worker-side Caddy proxy router.
// It lives entirely outside the runtime package — Caddy management is a worker
// concern, not a core runtime concern.
package caddy

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	workerdeploy "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/payload"
)

// ProxyRouter manages Caddy proxy routes on the local worker node.
// Construct it with New after the Caddy pod is running, then pass it to
// dispatch.Dispatch.
type ProxyRouter struct {
	pm proxy.ProxyManager
}

// New builds a ProxyRouter by discovering the Caddy admin port from the
// named pod via the runtime and pointing the HTTP client at it.
func New(ctx context.Context, rt runtime.Runtime) (*ProxyRouter, error) {
	adminPort, err := proxy.GetCaddyAdminPort(ctx, rt, workerdeploy.WorkerCaddyPodName)
	if err != nil {
		return nil, fmt.Errorf("caddy router: discover admin port: %w", err)
	}

	adminURL := fmt.Sprintf("http://localhost:%s", adminPort)
	pm := proxy.NewCaddyManager(adminURL, constants.CaddyServerName)

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
		return nil, pr.pm.RegisterRoute(ctx, r)

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
