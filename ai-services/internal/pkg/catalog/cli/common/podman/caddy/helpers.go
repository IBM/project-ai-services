package caddy

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/project-ai-services/ai-services/assets"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	domainSuffixEnvVar = "DOMAIN_SUFFIX"
	httpsPortEnvVar    = "CADDY_HTTPS_PORT"
	baseDirEnvVar      = "AI_SERVICES_BASE_DIR"
)

// ComputeDomainConfig computes the domain configuration from SSL certificates and domain name.
// Priority: certDomain > customDomain > hostIP.nip.io.
func ComputeDomainConfig(sslCertPath, sslKeyPath, domainName string) (string, error) {
	var domainSuffix string

	// If SSL certificate is provided, extract domain from it
	if sslCertPath != "" && sslKeyPath != "" {
		extractedDomain, err := utils.ExtractDomainFromCertificate(sslCertPath)
		if err != nil {
			return "", fmt.Errorf("failed to extract domain from certificate: %w", err)
		}
		domainSuffix = extractedDomain
	} else if domainName != "" {
		// Use provided domain name
		domainSuffix = domainName
	} else {
		// Default to hostIP.nip.io when no configuration is provided
		hostIP, err := utils.GetHostIP()
		if err != nil {
			return "", fmt.Errorf("failed to get host IP for domain suffix: %w", err)
		}
		domainSuffix = fmt.Sprintf("%s.nip.io", hostIP)
	}

	return domainSuffix, nil
}

// getCaddyAdminPort retrieves the host port mapped to Caddy's admin API (container port 2019).
func getCaddyAdminPort(runtime *podman.PodmanClient, podName string) (string, error) {
	pod, err := runtime.InspectPod(podName)
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
func getHTTPSPort(runtime *podman.PodmanClient, caddyPodName string) (string, error) {
	// Get pod details
	pod, err := runtime.InspectPod(caddyPodName)
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

// GetExistingConfigFromCatalogBackend retrieves the domain, HTTPS port, and base directory from the catalog-backend container.
// These values are used to validate that configuration hasn't changed during reconfigure operations.
func GetExistingConfigFromCatalogBackend(rt *podman.PodmanClient) (domain string, httpsPort string, baseDir string, err error) {
	// Construct the catalog-backend container name dynamically
	// Pattern: {AppName}--catalog-backend (e.g., "ai-services--catalog-backend")
	// This follows the Podman naming convention: {podName}-{containerName}
	catalogBackendContainerName := fmt.Sprintf("%s--catalog-backend", catalogconstants.CatalogAppName)

	// Inspect the catalog-backend container
	stats, err := containers.Inspect(rt.Context, catalogBackendContainerName, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to inspect catalog-backend container '%s': %w", catalogBackendContainerName, err)
	}

	if stats == nil || stats.Config == nil || stats.Config.Env == nil {
		return "", "", "", fmt.Errorf("invalid container stats when inspecting catalog-backend container")
	}

	// Extract DOMAIN_SUFFIX, HTTPS_PORT, and BASE_DIR from environment variables
	for _, envVar := range stats.Config.Env {
		// Environment variables are in format "KEY=VALUE"
		parts := strings.SplitN(envVar, "=", 2)
		if len(parts) == 2 {
			switch parts[0] {
			case domainSuffixEnvVar:
				domain = parts[1]
			case httpsPortEnvVar:
				httpsPort = parts[1]
			case baseDirEnvVar:
				baseDir = parts[1]
			}
			// Early exit if all values found
			if domain != "" && httpsPort != "" && baseDir != "" {
				break
			}
		}
	}

	if domain == "" {
		return "", "", "", fmt.Errorf("DOMAIN_SUFFIX environment variable not found in catalog-backend container")
	}

	if httpsPort == "" {
		return "", "", "", fmt.Errorf("CADDY_HTTPS_PORT environment variable not found in catalog-backend container")
	}

	if baseDir == "" {
		return "", "", "", fmt.Errorf("AI_SERVICES_BASE_DIR environment variable not found in catalog-backend container")
	}

	return domain, httpsPort, baseDir, nil
}

// ValidateReconfigureParameters validates that domain, HTTPS port, and base directory haven't changed during reconfigure.
// Simplified validation: always check all parameters against existing configuration.
func ValidateReconfigureParameters(rt *podman.PodmanClient, sslCertPath, sslKeyPath, domainName string, httpsPort int, baseDir string) error {
	// Get existing configuration from catalog-backend pod
	existingDomain, existingHTTPSPort, existingBaseDir, err := GetExistingConfigFromCatalogBackend(rt)
	if err != nil {
		return fmt.Errorf("failed to get existing configuration from catalog-backend: %w", err)
	}

	// Always validate domain during reconfigure
	newDomain, err := ComputeDomainConfig(sslCertPath, sslKeyPath, domainName)
	if err != nil {
		return fmt.Errorf("failed to compute domain suffix: %w", err)
	}

	// Validate domain matches
	if existingDomain != newDomain {
		return fmt.Errorf("domain change not allowed during reconfigure: existing=%s, new=%s. Please delete the catalog deployment and reconfigure fresh to change domain", existingDomain, newDomain)
	}

	// Always validate HTTPS port
	newPortStr := fmt.Sprintf("%d", httpsPort)
	if existingHTTPSPort != newPortStr {
		return fmt.Errorf("HTTPS port change not allowed during reconfigure: existing=%s, new=%s. Catalog is already configured with HTTPS port %s", existingHTTPSPort, newPortStr, existingHTTPSPort)
	}

	// Always validate base directory
	if existingBaseDir != baseDir {
		return fmt.Errorf("base directory change not allowed during reconfigure: existing=%s, new=%s. Please delete the catalog deployment and reconfigure fresh to change base directory", existingBaseDir, baseDir)
	}

	return nil
}

// Made with Bob
