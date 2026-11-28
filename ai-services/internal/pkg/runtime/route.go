package runtime

import (
	"context"
	"fmt"
)

// RouteHost returns the host the cluster assigned to the Route called name in the runtime's
// namespace (its .spec.host). The Route is found by name: a label is not a lookup key, because the
// gateway and the worker pod do not label their Routes the same way. It is an error if the Route
// does not exist or has no host yet.
func RouteHost(ctx context.Context, rt Runtime, name string) (string, error) {
	routes, err := rt.ListRoutes(ctx, "")
	if err != nil {
		return "", fmt.Errorf("list routes: %w", err)
	}

	for _, r := range routes {
		if r.Name != name {
			continue
		}

		if r.HostPort == "" {
			return "", fmt.Errorf("route %q exists but has no host assigned yet", name)
		}

		return r.HostPort, nil
	}

	return "", fmt.Errorf("route %q not found", name)
}
