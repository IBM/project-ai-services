package podman

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

const (
	domainSuffixEnvVar = "DOMAIN_SUFFIX"
	httpsPortEnvVar    = "CADDY_HTTPS_PORT"
	baseDirEnvVar      = "AI_SERVICES_BASE_DIR"
	certsDirName       = "certs"
)

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

// ValidateReconfigureParameters validates that domain, HTTPS port, base directory, and certificates haven't changed during reconfigure.
// This function performs all validation checks including certificate validation.
// Accepts pre-computed domainSuffix to avoid recomputing it.
func ValidateReconfigureParameters(rt *podman.PodmanClient, domainSuffix string, httpsPort int, baseDir, sslCertPath, sslKeyPath string) error {
	// Get existing configuration from catalog-backend pod
	existingDomain, existingHTTPSPort, existingBaseDir, err := GetExistingConfigFromCatalogBackend(rt)
	if err != nil {
		return fmt.Errorf("failed to get existing configuration from catalog-backend: %w", err)
	}

	// Validate domain matches (using pre-computed domain suffix)
	if existingDomain != domainSuffix {
		return fmt.Errorf("domain change not allowed during reconfigure: existing=%s, new=%s. Please uninstall the catalog deployment and re-run configure to change domain", existingDomain, domainSuffix)
	}

	// Always validate HTTPS port
	newPortStr := fmt.Sprintf("%d", httpsPort)
	if existingHTTPSPort != newPortStr {
		return fmt.Errorf("HTTPS port change not allowed during reconfigure: existing=%s, new=%s. Please uninstall the catalog deployment and re-run configure to change https port", existingHTTPSPort, newPortStr)
	}

	// Always validate base directory
	if existingBaseDir != baseDir {
		return fmt.Errorf("base directory change not allowed during reconfigure: existing=%s, new=%s. Please uninstall the catalog deployment and re-run configure to change base directory", existingBaseDir, baseDir)
	}

	// Validate certificate changes if SSL certificates are provided
	if sslCertPath != "" && sslKeyPath != "" {
		// Define staged certificate paths
		stagedCertPath := filepath.Join(baseDir, "common", "caddy", certsDirName, "tls.crt")
		stagedKeyPath := filepath.Join(baseDir, "common", "caddy", certsDirName, "tls.key")

		// Check if staged certificates exist from previous successful deployment
		_, certErr := os.Stat(stagedCertPath)
		_, keyErr := os.Stat(stagedKeyPath)

		if os.IsNotExist(certErr) || os.IsNotExist(keyErr) {
			// No staged certificates found - allow cert loading since domain is already validated
			return nil
		}

		// Staged certificates exist - compare content
		needsUpdate, err := caddy.CertificatesNeedUpdate(sslCertPath, sslKeyPath, stagedCertPath, stagedKeyPath)
		if err != nil {
			return fmt.Errorf("failed to check certificate status: %w", err)
		}

		if needsUpdate {
			// Certificates differ - block update
			return fmt.Errorf("certificate content change not allowed during reconfigure. Please reset cert")
		}
	}

	return nil
}

// Made with Bob
