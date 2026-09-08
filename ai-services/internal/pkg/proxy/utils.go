package proxy

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
)

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
// Domain is set to the subdomain (e.g. "catalog-api"). RegisterRoute appends
// DOMAIN_SUFFIX from the local environment to form the full hostname, so the
// catalog and every remote worker each use their own domain suffix.
func BuildRoutesFromAnnotation(routesAnnotation, podName string) ([]Route, error) {
	if routesAnnotation == "" {
		return nil, nil
	}

	routes := []Route{}

	for _, r := range strings.Split(routesAnnotation, ",") {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}

		parts, err := ParseRouteEntry(r)
		if err != nil {
			return nil, err
		}

		route := Route{
			ID:       parts.Subdomain,
			Domain:   parts.Subdomain,
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
