package podman

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

const (
	domainSuffixEnvVar = "DOMAIN_SUFFIX"
	httpsPortEnvVar    = "CADDY_HTTPS_PORT"
	baseDirEnvVar      = "AI_SERVICES_BASE_DIR"
	certsDirName       = "certs"
)

// GetExistingConfigFromCatalogBackend retrieves the domain, HTTPS port, and base directory from the catalog pod.
// These values are used to validate that configuration hasn't changed during reconfigure operations.
func GetExistingConfigFromCatalogBackend(rt runtime.Runtime) (domain string, httpsPort string, baseDir string, err error) {
	// Use getCatalogPodDetails to retrieve configuration from the catalog pod
	opts, _, err := getCatalogPodDetails(rt)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get catalog pod details: %w", err)
	}

	// Validate that all required configuration values were found
	if opts.DomainName == "" {
		return "", "", "", fmt.Errorf("DOMAIN_SUFFIX environment variable not found in catalog pod")
	}

	if opts.HttpsPort == 0 {
		return "", "", "", fmt.Errorf("CADDY_HTTPS_PORT environment variable not found in catalog pod")
	}

	if opts.BaseDir == "" {
		return "", "", "", fmt.Errorf("AI_SERVICES_BASE_DIR environment variable not found in catalog pod")
	}

	// Convert HttpsPort from int to string for return value
	httpsPortStr := strconv.Itoa(opts.HttpsPort)

	return opts.DomainName, httpsPortStr, opts.BaseDir, nil
}

// ValidateReconfigureParameters validates that domain, HTTPS port, base directory, and certificates haven't changed during reconfigure.
// This function performs all validation checks including certificate validation.
// Accepts pre-computed domainSuffix to avoid recomputing it.
func ValidateReconfigureParameters(rt runtime.Runtime, domainSuffix string, httpsPort int, baseDir, sslCertPath, sslKeyPath string) error {
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
