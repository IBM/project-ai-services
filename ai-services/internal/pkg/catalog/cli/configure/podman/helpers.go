package podman

import (
	"fmt"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/common/podman/caddy"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

const certsDirName = "certs"

// getExistingConfigFromCatalogBackend retrieves the existing configuration from the catalog pod.
// These values are used to validate that configuration hasn't changed during reconfigure operations.
func getExistingConfigFromCatalogBackend(rt runtime.Runtime) (*catalogUtils.PodmanConfigureOptions, error) {
	opts, _, err := catalogUtils.GetCatalogPodConfig(rt)
	if err != nil {
		return nil, fmt.Errorf("failed to get catalog pod details: %w", err)
	}

	if err := validateRequiredFields(opts); err != nil {
		return nil, err
	}

	return opts, nil
}

// validateRequiredFields validates that all required configuration values are present.
func validateRequiredFields(opts *catalogUtils.PodmanConfigureOptions) error {
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
func validateReconfigureParameters(rt runtime.Runtime, newOpts *catalogUtils.PodmanConfigureOptions, domainSuffix string) error {
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
func validateConfigParameters(existingOpts *catalogUtils.PodmanConfigureOptions, newOpts *catalogUtils.PodmanConfigureOptions, domainSuffix string) error {
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

// validateCertificateChanges prevents switching from custom certificates back to Caddy self-signed certificates.
// Allows updating custom certificate content (e.g., for expiry or renewal).
// Uses glob patterns to detect timestamped certificate files.
func validateCertificateChanges(opts *catalogUtils.PodmanConfigureOptions) error {
	// Define staged certificate directory
	certDir := filepath.Join(opts.BaseDir, "common", "caddy", certsDirName)

	// Check if any timestamped certificates exist from previous deployment
	stagedCerts, _ := filepath.Glob(filepath.Join(certDir, "tls-*.crt"))
	stagedKeys, _ := filepath.Glob(filepath.Join(certDir, "tls-*.key"))

	stagedCertExists := len(stagedCerts) > 0
	stagedKeyExists := len(stagedKeys) > 0

	// If no SSL paths provided in new config but staged certs exist, block cert type change
	if (opts.SSLCertPath == "" || opts.SSLKeyPath == "") && stagedCertExists && stagedKeyExists {
		return fmt.Errorf("certificate type change not allowed: custom certificates are already configured. Cannot switch to Caddy self-signed certificates during reconfigure. Please uninstall the catalog deployment and re-run configure to change certificate type")
	}

	// Allow all other scenarios:
	// - First run with Caddy certs (no SSL paths, no staged certs)
	// - First run with custom certs (SSL paths provided, no staged certs)
	// - Reconfigure with same custom certs (content matches)
	// - Reconfigure with updated custom certs (content differs - allow for cert renewal/expiry)
	// - Caddy self-signed to custom certs transition
	return nil
}

// validateDomainUnchanged validates that the domain hasn't changed from the existing configuration.
func validateDomainUnchanged(existingOpts *catalogUtils.PodmanConfigureOptions, sslCertPath, sslKeyPath string) error {
	// Compute the current domain configuration based on the provided SSL certificates
	// This uses the same logic as initial configuration
	currentDomainSuffix, err := caddy.ComputeDomainConfig(sslCertPath, sslKeyPath, "")
	if err != nil {
		return fmt.Errorf("failed to compute current domain: %w", err)
	}

	// Compare existing domain with current domain
	if existingOpts.DomainName != currentDomainSuffix {
		return fmt.Errorf("domain change detected: existing=%s, current=%s. Domain changes are not allowed during reset-certificate. Please uninstall the catalog deployment and re-run configure with the new domain", existingOpts.DomainName, currentDomainSuffix)
	}

	return nil
}

// Made with Bob
