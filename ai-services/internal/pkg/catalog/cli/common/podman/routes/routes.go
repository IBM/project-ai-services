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

// catalogAuthRules are the only catalog endpoints a remote worker may reach over the private
// ingress. Every other method and path under the prefix is not matched and returns 404.
var catalogAuthRules = []proxy.RouteRule{
	{Method: "GET", Path: "/.well-known/jwks.json"},
	{Method: "POST", Path: "/api/v1/auth/code/exchange"},
	{Method: "POST", Path: "/api/v1/auth/service-credential/token"},
}

// RegisterLiteLLMIngress exposes LiteLLM on the control-plane Caddy's private mTLS ingress, so
// services on a remote worker can reach it. The worker gateway registers the matching egress route
// on each remote worker when it connects. Registering an existing route is a no-op, so this is safe
// on reconfigure.
func RegisterLiteLLMIngress(ctx context.Context, caddyCtx *caddy.Context, routeInfos []caddy.TemplateRouteInfo) error {
	upstream, err := routeUpstream(routeInfos, catalogconstants.ModelGatewayRouteID)
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

// RegisterCatalogAuthIngress exposes the catalog's service-auth endpoints (JWKS, code exchange and
// service-credential token) on the control-plane Caddy's private mTLS ingress, so services on a
// remote worker reach the catalog without a public catalog-api address or a CA bundle. Only the
// exact (method, path) pairs in catalogAuthRules are routed. The worker gateway registers the
// matching egress route on each remote worker when it connects. Registering an existing route is
// a no-op, so this is safe on reconfigure.
func RegisterCatalogAuthIngress(ctx context.Context, caddyCtx *caddy.Context, routeInfos []caddy.TemplateRouteInfo) error {
	upstream, err := routeUpstream(routeInfos, catalogconstants.CatalogAPIRouteID)
	if err != nil {
		return err
	}

	proxyManager, err := caddyCtx.CreateProxyManager(ctx)
	if err != nil {
		return fmt.Errorf("failed to create proxy manager: %w", err)
	}

	if err := proxyManager.RegisterIngressRoute(ctx, proxy.IngressRoute{
		ID:           workerconstants.CatalogAuthIngressRouteID,
		PathPrefix:   workerconstants.CatalogAuthPathPrefix,
		Upstream:     upstream,
		Terminal:     true,
		AllowedRules: catalogAuthRules,
	}); err != nil {
		return fmt.Errorf("failed to register catalog-auth ingress route: %w", err)
	}

	return nil
}

// routeUpstream returns the "pod:port" of the route with the given ID, taken from the pod
// templates' routes annotations.
func routeUpstream(routeInfos []caddy.TemplateRouteInfo, routeID string) (string, error) {
	for _, info := range routeInfos {
		routes, err := proxy.BuildRoutesFromAnnotation(info.RoutesAnnotation, info.PodName)
		if err != nil {
			return "", fmt.Errorf("pod %s: %w", info.PodName, err)
		}

		for _, r := range routes {
			if r.ID == routeID {
				return r.Upstream, nil
			}
		}
	}

	return "", fmt.Errorf("no %q route found in the catalog templates", routeID)
}
