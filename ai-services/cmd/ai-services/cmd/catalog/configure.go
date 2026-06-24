package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/catalog/common"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure"
	catalogPodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	// Runtime type flag for catalog configure command.
	runtimeType string
	// Base directory flag for catalog configure command.
	baseDir string
	// SSL certificate flags for HTTPS configuration.
	domainName  string
	sslCertPath string
	sslKeyPath  string
	// HTTPS port flag for catalog configure command.
	httpsPort int
	// Reset password flag for catalog configure command.
	resetPasswordFlag bool
	// Reset podman auth secret for catalog configure command.
	resetPodmanAuthFlag bool
	// Reset certificate flag for catalog configure command.
	resetCertificateFlag bool
)

const (
	defaultHTTPSPort = 443
)

// NewConfigureCmd creates a new configure command for the catalog service.
func NewConfigureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Configure the catalog service",
		Long: `Configure and deploy the AI Services catalog service with the specified runtime and configuration.

The configure and deploy process will:
	- Deploys the catalog services.
	- Create a new admin user if one does not exist.
	- Sets up base directory structure for applications and models

The command also supports additional parameters to configure base directories, domain name, SSL/TLS certificates, HTTPS port, and reset flags to update passwords and certificates.

Examples:
	 # Configure catalog service for podman
	 ai-services catalog configure --runtime podman

	 # Configure with custom HTTPS port
	 ai-services catalog configure --runtime podman --https-port 8443`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			if resetPasswordFlag {
				return validateResetFlag(cmd, "reset-password")
			} else if resetPodmanAuthFlag {
				return validateResetFlag(cmd, "reset-podman-auth")
			} else if resetCertificateFlag {
				return validateResetCertificateFlags(cmd, "reset-certificate")
			}

			return validateConfigureFlags()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if resetPasswordFlag {
				return runResetPassword()
			} else if resetPodmanAuthFlag {
				return runResetPodmanAuth()
			} else if resetCertificateFlag {
				return runResetCertificate()
			}

			return runConfigure()
		},
	}

	configureConfigureFlags(cmd)
	configureResetFlags(cmd)

	return cmd
}

// runConfigure executes the catalog configuration process.
func runConfigure() error {
	var aiServicesDir string
	var err error

	// Use default base directory if not specified, otherwise validate
	if baseDir == "" {
		aiServicesDir = constants.DefaultBaseDir
	} else {
		aiServicesDir, err = utils.ValidateBaseDir(baseDir)
		if err != nil {
			return fmt.Errorf("invalid base directory '%s': %w", baseDir, err)
		}
	}

	logger.Debugf("Using base directory: %s\n", aiServicesDir)

	// create model directory
	modelPath := filepath.Join(aiServicesDir, "models")
	err = utils.CreateDir(modelPath)
	if err != nil {
		return fmt.Errorf("failed to create model directory: %w", err)
	}

	// Sanitize SSL certificate paths to prevent path traversal attacks
	cleanCertPath, cleanKeyPath := sanitizeSSLPaths(sslCertPath, sslKeyPath)

	return configure.Run(vars.RuntimeFactory.GetRuntimeType(), aiServicesDir, domainName, cleanCertPath, cleanKeyPath, httpsPort)
}

func validateResetFlag(cmd *cobra.Command, flagName string) error {
	if err := common.InitAndValidateRuntimeFlag(runtimeType); err != nil {
		return err
	}

	// Check that no configuration parameters are provided with reset flag
	var invalidFlags []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if f.Name == flagName || f.Name == constants.RuntimeFlag {
			// Skip reset flag and runtime parameter
			return
		}
		invalidFlags = append(invalidFlags, "--"+f.Name)
	})
	if len(invalidFlags) > 0 {
		return fmt.Errorf("the following flags cannot be used with --%s: %v", flagName, invalidFlags)
	}

	return nil
}

// validateConfigureFlags validates the configure command flags and initializes runtime.
func validateConfigureFlags() error {
	if err := common.InitAndValidateRuntimeFlag(runtimeType); err != nil {
		return err
	}

	// Validate SSL flags
	if err := validateSSLFlags(); err != nil {
		return err
	}

	// Validate HTTPS port range
	if httpsPort < 1 || httpsPort > 65535 {
		return fmt.Errorf("invalid HTTPS port %d: must be between 1 and 65535", httpsPort)
	}

	return nil
}

// validateSSLFlags validates SSL certificate and key flags.
func validateSSLFlags() error {
	// If no SSL cert/key provided, validation passes
	if sslCertPath == "" && sslKeyPath == "" {
		return nil
	}

	if err := checkSSLFlagsPaired(); err != nil {
		return err
	}

	warnIfBothCertAndDomainProvided()

	return validateSSLCertificates()
}

// checkSSLFlagsPaired ensures cert and key flags are used together.
func checkSSLFlagsPaired() error {
	if (sslCertPath != "" && sslKeyPath == "") || (sslCertPath == "" && sslKeyPath != "") {
		return fmt.Errorf("--ssl-cert and --ssl-key must be used together")
	}

	return nil
}

// warnIfBothCertAndDomainProvided warns user if both certificate and custom domain are provided.
func warnIfBothCertAndDomainProvided() {
	if sslCertPath != "" && sslKeyPath != "" && domainName != "" {
		fmt.Fprintf(os.Stderr, "Warning: Both SSL certificate and --domain-name provided. "+
			"The domain from the certificate will be used, and --domain-name will be ignored.\n\n")
	}
}

// validateSSLCertificates performs comprehensive validation of SSL certificates.
func validateSSLCertificates() error {
	// Validate certificate files exist and are readable
	if err := utils.ValidateCertificateFiles(sslCertPath, sslKeyPath); err != nil {
		return fmt.Errorf("certificate validation failed: %w", err)
	}

	// Validate certificate and key match
	if err := utils.ValidateCertificateKeyPair(sslCertPath, sslKeyPath); err != nil {
		return fmt.Errorf("certificate and key validation failed: %w", err)
	}

	// Validate wildcard certificate
	if err := utils.ValidateWildcardCertificate(sslCertPath); err != nil {
		return fmt.Errorf("wildcard certificate validation failed: %w", err)
	}

	return nil
}

func validateResetCertificateFlags(cmd *cobra.Command, flagName string) error {
	if err := common.InitAndValidateRuntimeFlag(runtimeType); err != nil {
		return err
	}

	// Require SSL certificate flags with reset-certificate
	if sslCertPath == "" || sslKeyPath == "" {
		return fmt.Errorf("--ssl-cert and --ssl-key are required when using --reset-certificate")
	}

	// Validate SSL certificate flags
	if err := validateSSLFlags(); err != nil {
		return err
	}

	// Check that no other configuration parameters are provided with reset-certificate flag
	// Allow ssl-cert and ssl-key since they are required for this operation
	var invalidFlags []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if f.Name == flagName || f.Name == constants.RuntimeFlag ||
			f.Name == "ssl-cert" || f.Name == "ssl-key" {
			// Skip reset flag, runtime parameter, and required SSL flags
			return
		}
		invalidFlags = append(invalidFlags, "--"+f.Name)
	})
	if len(invalidFlags) > 0 {
		return fmt.Errorf("the following flags cannot be used with --%s: %v", flagName, invalidFlags)
	}

	return nil
}

func runResetCertificate() error {
	// Sanitize SSL certificate paths to prevent path traversal attacks
	cleanCertPath, cleanKeyPath := sanitizeSSLPaths(sslCertPath, sslKeyPath)

	// Call ResetCatalogCertificate with certificate paths
	return catalogPodman.ResetCatalogCertificate(cleanCertPath, cleanKeyPath)
}

// configureConfigureFlags configures the flags for the configure command.
func configureConfigureFlags(cmd *cobra.Command) {
	// Add runtime flag as required
	common.ConfigureRuntimeFlag(cmd, &runtimeType)

	// Add basedir flag
	cmd.Flags().StringVar(
		&baseDir,
		"basedir",
		"",
		"Base directory for AI services data (applications, models, cache).\n"+
			"Example: --basedir /custom/path\n",
	)

	// Add HTTPS port flag
	cmd.Flags().IntVar(
		&httpsPort,
		"https-port",
		defaultHTTPSPort,
		"Custom HTTPS port to expose the service endpoints externally (podman runtime only).\n"+
			"Example: --https-port 8443\n",
	)

	// SSL/TLS certificate configuration flags
	cmd.Flags().StringVar(
		&domainName,
		"domain-name",
		"",
		"Custom domain name for self-signed certificates (podman runtime only).\n"+
			"If not provided, uses wildcard DNS format: <service>.<ip>.nip.io\n"+
			"If a custom SSL certificate/key pair is provided, the domain is extracted from the certificate and the --domain flag is ignored.\n"+
			"Example: --domain-name example.com generates certs for *.example.com\n",
	)

	cmd.Flags().StringVar(
		&sslCertPath,
		"ssl-cert",
		"",
		"Path to user-provided SSL certificate (optional).\n"+
			"Must be used together with --ssl-key.\n"+
			"Certificate must contain wildcard SAN entry (e.g., *.example.com).\n"+
			"Example: --ssl-cert /path/to/cert.pem\n",
	)

	cmd.Flags().StringVar(
		&sslKeyPath,
		"ssl-key",
		"",
		"Path to user-provided SSL private key (optional).\n"+
			"Must be used together with --ssl-cert.\n"+
			"Example: --ssl-key /path/to/key.pem\n",
	)
}

func configureResetFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(
		&resetPasswordFlag,
		"reset-password",
		false,
		"Reset the password for the admin user",
	)

	cmd.Flags().BoolVar(
		&resetPodmanAuthFlag,
		"reset-podman-auth",
		false,
		"Reset podman authentication using the system's current auth.json.",
	)

	cmd.Flags().BoolVar(
		&resetCertificateFlag,
		"reset-certificate",
		false,
		"Reset the Caddy SSL certificates by loading new custom certificates.\n"+
			"Requires --ssl-cert and --ssl-key flags to specify the new certificate files.\n"+
			"This will reload the certificates in Caddy without restarting the pod.\n"+
			"Example:\n"+
			"  ai-services catalog configure --runtime podman --reset-certificate --ssl-cert /path/to/cert.pem --ssl-key /path/to/key.pem\n",
	)
}

func runResetPassword() error {
	return catalogPodman.ResetCatalogPassword()
}

func runResetPodmanAuth() error {
	return catalogPodman.ResetPodmanAuth()
}

// sanitizeSSLPaths sanitizes SSL certificate and key paths to prevent path traversal attacks.
// Only cleans if paths are provided (filepath.Clean("") returns ".").
// Returns cleaned cert path and cleaned key path.
func sanitizeSSLPaths(certPath, keyPath string) (string, string) {
	cleanCertPath := ""
	cleanKeyPath := ""

	if certPath != "" {
		cleanCertPath = filepath.Clean(certPath)
	}
	if keyPath != "" {
		cleanKeyPath = filepath.Clean(keyPath)
	}

	return cleanCertPath, cleanKeyPath
}

// Made with Bob
