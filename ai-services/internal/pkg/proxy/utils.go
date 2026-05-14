package proxy

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
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

// BuildRoutesFromConfig builds routes by reading routes_file.yaml configuration.
// This approach uses static configuration to define routing without needing pod inspection.
func BuildRoutesFromConfig(tp templates.Template, appName, hostIP string) ([]Route, error) {
	// Load routes configuration
	routesConfig, err := tp.LoadRoutesFile(appName)
	if err != nil {
		return nil, fmt.Errorf("failed to load routes config: %w", err)
	}

	// Pre-calculate total number of routes for efficient allocation
	totalRoutes := 0
	for _, podConfig := range routesConfig.Routes {
		totalRoutes += len(podConfig.Services)
	}
	routes := make([]Route, 0, totalRoutes)

	// Process each pod in the configuration
	for _, podConfig := range routesConfig.Routes {
		podName := podConfig.Pod

		// Process each service in the pod
		for _, svc := range podConfig.Services {
			// Extract container port number (remove /tcp suffix)
			containerPort := strings.Split(svc.ContainerPort, "/")[0]

			// Build route
			route := Route{
				ID:       fmt.Sprintf("%s--%s", podName, svc.Subdomain),
				Domain:   fmt.Sprintf("%s.%s.nip.io", svc.Subdomain, hostIP),
				Upstream: fmt.Sprintf("%s:%s", podName, containerPort),
				Terminal: true,
			}
			routes = append(routes, route)
		}
	}

	return routes, nil
}

// Made with Bob
