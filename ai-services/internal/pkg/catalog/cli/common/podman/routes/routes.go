// Package routes manages control-plane proxy routes for private mTLS communication with workers.
package routes

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

// RegisterLiteLLMIngress exposes LiteLLM on the control-plane Caddy's private mTLS ingress, so
// services on a remote worker can reach it. The worker gateway registers the matching egress route
// on each remote worker when it connects. Registering an existing route is a no-op, so this is safe
// on reconfigure.
func RegisterLiteLLMIngress(ctx context.Context, caddyCtx *caddy.Context, routeInfos []caddy.TemplateRouteInfo) error {
	upstream, err := liteLLMUpstream(routeInfos)
	if err != nil {
		return err
	}

	proxyManager, err := caddyCtx.CreateProxyManager(ctx)
	if err != nil {
		return fmt.Errorf("failed to create proxy manager: %w", err)
	}

	if err := proxyManager.RegisterIngressRoute(ctx, proxy.IngressRoute{
		ID:         workerconstants.LiteLLMIngressRouteID,
		PathPrefix: workerconstants.LiteLLMPathPrefix,
		Upstream:   upstream,
		Terminal:   true,
	}); err != nil {
		return fmt.Errorf("failed to register LiteLLM ingress route: %w", err)
	}

	return nil
}

// liteLLMUpstream returns the "pod:port" of the LiteLLM pod, taken from its model-gateway route.
func liteLLMUpstream(routeInfos []caddy.TemplateRouteInfo) (string, error) {
	for _, info := range routeInfos {
		routes, err := proxy.BuildRoutesFromAnnotation(info.RoutesAnnotation, info.PodName)
		if err != nil {
			return "", fmt.Errorf("pod %s: %w", info.PodName, err)
		}

		for _, r := range routes {
			if r.ID == catalogconstants.ModelGatewayRouteID {
				return r.Upstream, nil
			}
		}
	}

	return "", fmt.Errorf("no %q route found in the catalog templates", catalogconstants.ModelGatewayRouteID)
}
