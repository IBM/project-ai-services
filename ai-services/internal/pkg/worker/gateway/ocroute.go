package gateway

import (
	"context"
	"fmt"

	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	runtimeopenshift "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
)

const (
	// gatewayRouteName is the OpenShift Route name for the worker-gateway passthrough route.
	gatewayRouteName = "catalog-worker-gateway"
)

// GatewayRouteHost looks up the live OpenShift passthrough route for the worker
// gateway and returns its hostname (.spec.host). It is used to embed the correct
// DNS SAN in the auto-generated server certificate so that worker nodes can verify
// the gateway TLS connection regardless of which cluster the catalog is running on.
//
// If no route named "catalog-worker-gateway" is found, or the route has no host
// assigned yet, an error is returned.
func GatewayRouteHost(ctx context.Context) (string, error) {
	oc, err := runtimeopenshift.NewOpenshiftClientWithNamespace(catalogconstants.CatalogAppName)
	if err != nil {
		return "", fmt.Errorf("worker gateway: create OpenShift client to look up gateway route: %w", err)
	}

	labelSelector := fmt.Sprintf("%s=%s", constants.ApplicationAnnotationKey, catalogconstants.CatalogAppName)
	routes, err := oc.ListRoutes(ctx, labelSelector)
	if err != nil {
		return "", fmt.Errorf("worker gateway: list routes (selector=%s): %w", labelSelector, err)
	}

	for _, r := range routes {
		if r.Name == gatewayRouteName {
			if r.HostPort == "" {
				return "", fmt.Errorf("worker gateway: route %q exists but has no host assigned yet", gatewayRouteName)
			}

			return r.HostPort, nil
		}
	}

	return "", fmt.Errorf("worker gateway: route %q not found (selector=%s)", gatewayRouteName, labelSelector)
}
