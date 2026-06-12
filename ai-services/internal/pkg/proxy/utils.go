package proxy

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// GetCaddyAdminPort retrieves the host port mapped to Caddy's admin API (container port 2019).
func GetCaddyAdminPort(rt runtime.Runtime, podName string) (string, error) {
	pod, err := rt.InspectPod(podName)
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

// RouteEntryParts represents the parsed components of a route entry.
type RouteEntryParts struct {
	Port      string
	Subdomain string
	Type      string
}

// ParseRouteEntry parses a single route entry string in the format "port:subdomain:type".
// Returns the parsed parts or an error if the format is invalid.
func ParseRouteEntry(routeEntry string) (*RouteEntryParts, error) {
	const expectedParts = 3

	routeEntry = strings.TrimSpace(routeEntry)
	if routeEntry == "" {
		return nil, fmt.Errorf("route entry is empty")
	}

	// Split by colon
	parts := strings.Split(routeEntry, ":")
	if len(parts) != expectedParts {
		return nil, fmt.Errorf("invalid route format '%s': expected 'port:subdomain:type', got %d parts", routeEntry, len(parts))
	}

	port := strings.TrimSpace(parts[0])
	subdomain := strings.TrimSpace(parts[1])
	routeType := strings.ToLower(strings.TrimSpace(parts[2]))

	if port == "" {
		return nil, fmt.Errorf("invalid route '%s': port cannot be empty", routeEntry)
	}
	if subdomain == "" {
		return nil, fmt.Errorf("invalid route '%s': subdomain cannot be empty", routeEntry)
	}
	if routeType == "" {
		return nil, fmt.Errorf("invalid route '%s': type cannot be empty", routeEntry)
	}

	return &RouteEntryParts{
		Port:      port,
		Subdomain: subdomain,
		Type:      routeType,
	}, nil
}

// BuildRoutesFromAnnotation parses a routes annotation string and builds Route objects.
// The annotation format is: "port:subdomain:type, port:subdomain:type, ...".
// Example: "8081:catalog-ui:ui, 8080:catalog-api:api".
// The domainSuffix is pre-computed (e.g., "example.com" or "192.168.1.100.nip.io").
func BuildRoutesFromAnnotation(routesAnnotation, domainSuffix, podName string) ([]Route, error) {
	if routesAnnotation == "" {
		return nil, nil
	}

	routes := []Route{}

	// Parse routes annotation (format: "port:subdomain:type, port:subdomain:type, ...")
	for _, r := range strings.Split(routesAnnotation, ",") {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}

		// Parse the route entry using shared helper
		parts, err := ParseRouteEntry(r)
		if err != nil {
			return nil, err
		}

		// Build route - use pod name as upstream since containers are in the same pod
		// Domain is simply: subdomain.domainSuffix
		// Route ID uses just the subdomain since subdomains are already globally unique
		route := Route{
			ID:       parts.Subdomain,
			Domain:   fmt.Sprintf("%s.%s", parts.Subdomain, domainSuffix),
			Upstream: fmt.Sprintf("%s:%s", podName, parts.Port),
			Terminal: true,
			Type:     parts.Type,
		}
		routes = append(routes, route)
	}

	return routes, nil
}

// FindCaddyPodNameFromTemplates finds the Caddy pod name by looking for the pod with component=proxy label in templates.
func FindCaddyPodNameFromTemplates(tp templates.Template, appTemplateName, catalogAppName string, argParams map[string]string) (string, error) {
	// Load all templates
	tmpls, err := tp.LoadAllTemplates(appTemplateName)
	if err != nil {
		return "", fmt.Errorf("failed to load templates: %w", err)
	}

	// Loop through all templates to find the Caddy pod
	for templateName := range tmpls {
		podSpec, err := tp.LoadPodTemplateWithValues(appTemplateName, templateName, catalogAppName, nil, argParams)
		if err != nil {
			return "", fmt.Errorf("failed to load template %s: %w", templateName, err)
		}

		// Check if this is the Caddy pod (component=proxy label)
		if podSpec.Labels != nil {
			if component, ok := podSpec.Labels["ai-services.io/component"]; ok && component == "proxy" {
				return podSpec.Name, nil
			}
		}
	}

	return "", fmt.Errorf("no Caddy pod found with component=proxy label in templates")
}

// Made with Bob
