package proxy

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// GetCaddyAdminPort retrieves the host port mapped to Caddy's admin API (container port 2019).
func GetCaddyAdminPort(rt runtime.Runtime, appName string) (string, error) {
	caddyPodName := fmt.Sprintf("%s--caddy", appName)
	pod, err := rt.InspectPod(caddyPodName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect Caddy pod: %w", err)
	}

	// Get port mappings from the Ports field
	// Ports is a map[string][]string where key is "containerPort/protocol" and value is list of host ports
	// Example: {"2019/tcp": ["37249"], "443/tcp": ["39341"]}
	for containerPort, hostPorts := range pod.Ports {
		// Check if this is the admin API port (2019)
		if strings.HasPrefix(containerPort, "2019/") && len(hostPorts) > 0 {
			return hostPorts[0], nil
		}
	}

	return "", fmt.Errorf("admin port mapping not found in pod ports")
}

// BuildRoutesFromAnnotation parses a routes annotation string and builds Route objects.
// The annotation format is: "port:subdomain, port:subdomain, ...".
// Example: "8081:catalog-ui, 8080:catalog-api".
func BuildRoutesFromAnnotation(routesAnnotation, hostIP, podName string) ([]Route, error) {
	if routesAnnotation == "" {
		return nil, nil
	}

	routes := []Route{}
	const expectedParts = 2

	// Parse routes annotation (format: "port:subdomain, port:subdomain, ...")
	for _, r := range strings.Split(routesAnnotation, ",") {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}

		// Split by colon
		parts := strings.Split(r, ":")
		if len(parts) != expectedParts {
			continue
		}

		port := strings.TrimSpace(parts[0])
		subdomain := strings.TrimSpace(parts[1])

		if port == "" || subdomain == "" {
			continue
		}

		// Build route - use pod name as upstream since containers are in the same pod
		route := Route{
			ID:       fmt.Sprintf("%s--%s", podName, subdomain),
			Domain:   fmt.Sprintf("%s.%s.nip.io", subdomain, hostIP),
			Upstream: fmt.Sprintf("%s:%s", podName, port),
			Terminal: true,
		}
		routes = append(routes, route)
	}

	return routes, nil
}

// Made with Bob
