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

// DomainConfig holds configuration for domain generation with priority-based selection.
type DomainConfig struct {
	HostIP       string // Used for nip.io-based domains (lowest priority)
	CustomDomain string // User-provided custom domain (medium priority)
	CertDomain   string // Domain extracted from SSL certificate (highest priority)
}

// BuildRouteDomain generates a domain name for a route using priority-based selection:
// 1. CertDomain (highest priority) - from wildcard SSL certificate
// 2. CustomDomain (medium priority) - user-provided via --domain-name flag
// 3. HostIP with nip.io (lowest priority) - default fallback for self-signed certificates.
func BuildRouteDomain(subdomain string, config DomainConfig) string {
	// Priority 1: Certificate domain (from wildcard cert)
	if config.CertDomain != "" {
		return fmt.Sprintf("%s.%s", subdomain, config.CertDomain)
	}

	// Priority 2: Custom domain (user-provided)
	if config.CustomDomain != "" {
		return fmt.Sprintf("%s.%s", subdomain, config.CustomDomain)
	}

	// Priority 3: nip.io with host IP (default fallback)
	return fmt.Sprintf("%s.%s.nip.io", subdomain, config.HostIP)
}

// BuildRoutesFromAnnotation parses a routes annotation string and builds Route objects.
// The annotation format is: "port:subdomain, port:subdomain, ...".
// Example: "8081:catalog-ui, 8080:catalog-api".
func BuildRoutesFromAnnotation(routesAnnotation, hostIP, podName string, domainConfig *DomainConfig) ([]Route, error) {
	if routesAnnotation == "" {
		return nil, nil
	}

	routes := []Route{}
	const expectedParts = 2

	// Use provided domain configuration or create default with HostIP
	var config DomainConfig
	if domainConfig != nil {
		config = *domainConfig
		// Ensure HostIP is set for fallback
		if config.HostIP == "" {
			config.HostIP = hostIP
		}
	} else {
		config = DomainConfig{
			HostIP: hostIP,
		}
	}

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
			Domain:   BuildRouteDomain(subdomain, config),
			Upstream: fmt.Sprintf("%s:%s", podName, port),
			Terminal: true,
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
