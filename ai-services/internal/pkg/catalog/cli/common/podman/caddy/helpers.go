package caddy

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

// getCaddyAdminPort retrieves the host port mapped to Caddy's admin API (container port 2019).
func getCaddyAdminPort(ctx context.Context, podName string) (string, error) {
	pc, err := podman.NewPodmanClient()
	if err != nil {
		return "", fmt.Errorf("failed to initialize podman client: %w", err)
	}

	pod, err := pc.InspectPod(ctx, podName)
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

// getHTTPSPort retrieves the HTTPS port from the Caddy pod.
func getHTTPSPort(ctx context.Context, runtime *podman.PodmanClient, caddyPodName string) (string, error) {
	// Get pod details
	pod, err := runtime.InspectPod(ctx, caddyPodName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect Caddy pod: %w", err)
	}

	// Look for the HTTPS port mapping
	// Ports is a map[string][]string where key is "containerPort/protocol" (e.g., "443/tcp")
	// and value is list of host ports
	httpsPortKey := proxy.DefaultHTTPSPort + "/tcp"
	if hostPorts, ok := pod.Ports[httpsPortKey]; ok && len(hostPorts) > 0 {
		return hostPorts[0], nil
	}

	// Also check without protocol suffix for compatibility
	if hostPorts, ok := pod.Ports[proxy.DefaultHTTPSPort]; ok && len(hostPorts) > 0 {
		return hostPorts[0], nil
	}

	// Fallback: search through all port mappings
	for portKey, hostPorts := range pod.Ports {
		if strings.HasPrefix(portKey, proxy.DefaultHTTPSPort+"/") && len(hostPorts) > 0 {
			return hostPorts[0], nil
		}
	}

	return "", fmt.Errorf("HTTPS port not found in Caddy pod")
}

// GetCaddyFileContent returns caddy file content.
func GetCaddyFileContent() (string, error) {
	// Read the Caddyfile template
	caddyfileContent, err := assets.CatalogFS.ReadFile("catalog/podman/Caddyfile.tmpl")
	if err != nil {
		return "", fmt.Errorf("failed to read Caddyfile template: %w", err)
	}

	// Parse the Caddyfile as a template
	tmpl, err := template.New("Caddyfile.tmpl").Parse(string(caddyfileContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse Caddyfile template: %w", err)
	}

	// Prepare template data with the server name constant
	templateData := map[string]any{
		"CaddyServerName": constants.CaddyServerName,
	}

	// Execute the template
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, templateData); err != nil {
		return "", fmt.Errorf("failed to execute Caddyfile template: %w", err)
	}

	return rendered.String(), nil
}

// Made with Bob
