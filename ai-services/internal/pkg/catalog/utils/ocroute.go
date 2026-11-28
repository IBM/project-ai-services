package utils

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/catalogns"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeopenshift "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
)

const (
	// gatewayRouteName is the OpenShift Route name for the worker-gateway passthrough route.
	gatewayRouteName = "catalog-worker-gateway"
	// modelGatewayRouteName is the OpenShift Route name for the LiteLLM model gateway.
	modelGatewayRouteName = "model-gateway"
	// caddyRouteName is the OpenShift Route name for the control-plane Caddy private ingress.
	caddyRouteName = catalogconstants.CatalogAppName + "--caddy"
)

// GatewayRouteHost looks up the live OpenShift passthrough route for the worker
// gateway and returns its hostname (.spec.host). It is used to embed the correct
// DNS SAN in the auto-generated server certificate so that worker nodes can verify
// the gateway TLS connection regardless of which cluster the catalog is running on.
//
// If no route named "catalog-worker-gateway" is found, or the route has no host
// assigned yet, an error is returned.
func GatewayRouteHost(ctx context.Context) (string, error) {
	return routeHost(ctx, gatewayRouteName)
}

// CaddyRouteHost looks up the live OpenShift passthrough route of the control-plane Caddy and
// returns its hostname (.spec.host). Remote workers dial it for the private ingress (LiteLLM), so
// it has to be in the gateway server certificate's SANs and is the SNI the worker's egress route
// verifies.
//
// If no route named "ai-services--caddy" is found, or the route has no host assigned yet, an error is
// returned.
func CaddyRouteHost(ctx context.Context) (string, error) {
	return routeHost(ctx, caddyRouteName)
}

// ModelGatewayRouteHost looks up the live OpenShift route for the LiteLLM model gateway
// and returns its hostname (.spec.host).
//
// If no route named "model-gateway" is found, or the route has no host assigned yet,
// an error is returned.
func ModelGatewayRouteHost(ctx context.Context) (string, error) {
	return routeHost(ctx, modelGatewayRouteName)
}

func routeHost(ctx context.Context, routeName string) (string, error) {
	oc, err := runtimeopenshift.NewOpenshiftClientWithNamespace(catalogns.Get())
	if err != nil {
		return "", fmt.Errorf("create OpenShift client to look up route %q: %w", routeName, err)
	}

	return runtime.RouteHost(ctx, oc, routeName)
}
