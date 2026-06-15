package podman

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

const certsDirName = "certs"

// getExistingConfigFromCatalogBackend retrieves the existing configuration from the catalog pod.
// These values are used to validate that configuration hasn't changed during reconfigure operations.
func getExistingConfigFromCatalogBackend(rt runtime.Runtime) (*PodmanConfigureOptions, error) {
	opts, _, err := getCatalogPodDetails(rt)
	if err != nil {
		return nil, fmt.Errorf("failed to get catalog pod details: %w", err)
	}

	if err := validateRequiredFields(opts); err != nil {
		return nil, err
	}

	return opts, nil
}

// validateRequiredFields validates that all required configuration values are present.
func validateRequiredFields(opts *PodmanConfigureOptions) error {
	if opts.DomainName == "" {
		return fmt.Errorf("DOMAIN_SUFFIX environment variable not found in catalog pod")
	}
	if opts.HttpsPort == 0 {
		return fmt.Errorf("CADDY_HTTPS_PORT environment variable not found in catalog pod")
	}
	if opts.BaseDir == "" {
		return fmt.Errorf("AI_SERVICES_BASE_DIR environment variable not found in catalog pod")
	}

	return nil
}

// validateReconfigureParameters validates that domain, HTTPS port, base directory, and certificates haven't changed during reconfigure.
// This function performs all validation checks including certificate validation.
func validateReconfigureParameters(rt runtime.Runtime, newOpts *PodmanConfigureOptions, domainSuffix string) error {
	// Get existing configuration from catalog-backend pod
	existingOpts, err := getExistingConfigFromCatalogBackend(rt)
	if err != nil {
		return fmt.Errorf("failed to get existing configuration from catalog-backend: %w", err)
	}

	// Validate configuration parameters haven't changed
	if err := validateConfigParameters(existingOpts, newOpts, domainSuffix); err != nil {
		return err
	}

	// Validate certificate changes if SSL certificates are provided

	return validateCertificateChanges(newOpts)
}

// validateConfigParameters validates domain, HTTPS port, and base directory haven't changed.
func validateConfigParameters(existingOpts *PodmanConfigureOptions, newOpts *PodmanConfigureOptions, domainSuffix string) error {
	if existingOpts.DomainName != domainSuffix {
		return fmt.Errorf("domain change not allowed during reconfigure: existing=%s, new=%s. Please uninstall the catalog deployment and re-run configure to change domain", existingOpts.DomainName, domainSuffix)
	}

	if existingOpts.HttpsPort != newOpts.HttpsPort {
		return fmt.Errorf("HTTPS port change not allowed during reconfigure: existing=%d, new=%d. Please uninstall the catalog deployment and re-run configure to change https port", existingOpts.HttpsPort, newOpts.HttpsPort)
	}

	if existingOpts.BaseDir != newOpts.BaseDir {
		return fmt.Errorf("base directory change not allowed during reconfigure: existing=%s, new=%s. Please uninstall the catalog deployment and re-run configure to change base directory", existingOpts.BaseDir, newOpts.BaseDir)
	}

	return nil
}

// validateCertificateChanges checks if certificate content has changed during reconfigure.
func validateCertificateChanges(opts *PodmanConfigureOptions) error {
	if opts.SSLCertPath == "" || opts.SSLKeyPath == "" {
		return nil
	}

	// Define staged certificate paths
	stagedCertPath := filepath.Join(opts.BaseDir, "common", "caddy", certsDirName, "tls.crt")
	stagedKeyPath := filepath.Join(opts.BaseDir, "common", "caddy", certsDirName, "tls.key")

	// Check if both staged certificates exist from previous successful deployment
	if _, err := os.Stat(stagedCertPath); os.IsNotExist(err) {
		return nil
	}
	if _, err := os.Stat(stagedKeyPath); os.IsNotExist(err) {
		return nil
	}

	// Staged certificates exist - compare content
	needsUpdate, err := caddy.CertificatesNeedUpdate(opts.SSLCertPath, opts.SSLKeyPath, stagedCertPath, stagedKeyPath)
	if err != nil {
		return fmt.Errorf("failed to check certificate status: %w", err)
	}

	if needsUpdate {
		// Certificates differ - block update
		return fmt.Errorf("certificate content change not allowed during reconfigure. Please reset cert")
	}

	return nil
}

// Made with Bob
