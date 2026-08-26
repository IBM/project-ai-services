package caddy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/project-ai-services/ai-services/assets"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

// getCaddyAdminPort retrieves the host port mapped to Caddy's admin API (container port 2019).
func getCaddyAdminPort(ctx context.Context, runtime *podman.PodmanClient, podName string) (string, error) {
	pod, err := runtime.InspectPod(ctx, podName)
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
	httpsPortKey := catalogconstants.DefaultHTTPSPort + "/tcp"
	if hostPorts, ok := pod.Ports[httpsPortKey]; ok && len(hostPorts) > 0 {
		return hostPorts[0], nil
	}

	// Also check without protocol suffix for compatibility
	if hostPorts, ok := pod.Ports[catalogconstants.DefaultHTTPSPort]; ok && len(hostPorts) > 0 {
		return hostPorts[0], nil
	}

	// Fallback: search through all port mappings
	for portKey, hostPorts := range pod.Ports {
		if strings.HasPrefix(portKey, catalogconstants.DefaultHTTPSPort+"/") && len(hostPorts) > 0 {
			return hostPorts[0], nil
		}
	}

	return "", fmt.Errorf("HTTPS port not found in Caddy pod")
}

// GenerateCaddyfile copies the static Caddyfile to the caddy directory.
func GenerateCaddyfile(baseDir string) error {
	// Read the Caddyfile template
	caddyfileContent, err := assets.CatalogFS.ReadFile("catalog/podman/Caddyfile.tmpl")
	if err != nil {
		return fmt.Errorf("failed to read Caddyfile template: %w", err)
	}

	// Parse the Caddyfile as a template
	tmpl, err := template.New("Caddyfile.tmpl").Parse(string(caddyfileContent))
	if err != nil {
		return fmt.Errorf("failed to parse Caddyfile template: %w", err)
	}

	// Prepare template data with the server name constant
	templateData := map[string]any{
		"CaddyServerName": constants.CaddyServerName,
	}

	// Execute the template
	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, templateData); err != nil {
		return fmt.Errorf("failed to execute Caddyfile template: %w", err)
	}

	// Ensure directory exists and write Caddyfile
	caddyDir := filepath.Join(baseDir, "common", "caddy")
	if err := os.MkdirAll(caddyDir, dirPerm); err != nil {
		return fmt.Errorf("failed to create caddy directory: %w", err)
	}

	caddyfilePath := filepath.Join(caddyDir, "Caddyfile")
	if err := os.WriteFile(caddyfilePath, rendered.Bytes(), filePerm); err != nil {
		return fmt.Errorf("failed to write Caddyfile: %w", err)
	}

	return nil
}

// Made with Bob
