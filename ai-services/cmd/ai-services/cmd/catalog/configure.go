package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/catalog/common"
	catalogPodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure/podman"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/flagvalidator"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// Variables for flags placeholder.
var (
	// common flags.
	// Runtime type flag for catalog configure command.
	runtimeType string
	// Reset password flag for catalog configure command.
	resetPasswordFlag bool

	// podman flags.
	// Base directory flag for catalog configure command.
	baseDir string
	// SSL certificate flags for HTTPS configuration.
	domainName  string
	sslCertPath string
	sslKeyPath  string
	// HTTPS port flag for catalog configure command.
	httpsPort int
	// Reset podman auth secret for catalog configure command.
	resetPodmanAuthFlag bool
	// Reset certificate flag for catalog configure command.
	resetCertificateFlag bool
)

const (
	defaultHTTPSPort = 443
)

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Configure the catalog service",
	Long: `Configure and deploy the AI Services catalog service with the specified runtime.

This command performs the following operations:
  - Deploys the catalog services
  - Creates an admin user (if not already present)
  - Initializes directory structure for applications and models

Additional configuration options include base directory customization, domain name setup,
SSL/TLS certificate management, HTTPS port configuration, and credential/certificate reset capabilities.`,
	Example: `  # Configure catalog service for podman
  ai-services catalog configure --runtime podman

  # Configure with custom HTTPS port
  ai-services catalog configure --runtime podman --https-port 8443`,
	Args: cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		if err := common.InitAndValidateRuntimeFlag(runtimeType); err != nil {
			return err
		}

		// Reject runtime-scoped flags early.
		if err := buildFlagValidator().Validate(cmd); err != nil {
			return err
		}

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

// NewConfigureCmd returns the configure command for the catalog service.
func NewConfigureCmd() *cobra.Command {
	return configureCmd
}

func init() {
	initConfigureCommonFlags()
	initConfigurePodmanFlags()
}

// runConfigure executes the catalog configuration process.
func runConfigure() error {
	rt := vars.RuntimeFactory.GetRuntimeType()
	ctx := context.Background()
	// Deploy catalog service based on runtime
	switch rt {
	case types.RuntimeTypePodman:
		// Resolve base directory: fall back to default when not provided.
		aiServicesDir, err := resolveBaseDir(baseDir)
		if err != nil {
			return err
		}

		// Create the models directory under the base dir.
		modelPath := filepath.Join(aiServicesDir, "models")
		if err := utils.CreateDir(modelPath); err != nil {
			return fmt.Errorf("failed to create model directory: %w", err)
		}

		opts := catalogUtils.PodmanConfigureOptions{
			BaseDir:     baseDir,
			DomainName:  domainName,
			SSLCertPath: catalogUtils.SanitizeFilePath(sslCertPath),
			SSLKeyPath:  catalogUtils.SanitizeFilePath(sslKeyPath),
			HttpsPort:   httpsPort,
		}

		return catalogPodman.DeployCatalog(ctx, opts)

	case types.RuntimeTypeOpenShift:
		return fmt.Errorf("openshift runtime is not yet supported for catalog configure")

	default:
		return fmt.Errorf("unsupported runtime type: %s", rt)
	}
}

// resolveBaseDir returns the validated base directory, falling back to the default.
func resolveBaseDir(baseDir string) (string, error) {
	if baseDir == "" {
		return constants.DefaultBaseDir, nil
	}

	resolved, err := utils.ValidateBaseDir(baseDir)
	if err != nil {
		return "", fmt.Errorf("invalid base directory '%s': %w", baseDir, err)
	}

	return resolved, nil
}

func validateResetFlag(cmd *cobra.Command, flagName string) error {
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

// validateConfigureFlags validates the configure command flags.
func validateConfigureFlags() error {
	// Validate SSL flags
	if vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypePodman {
		if err := validateSSLFlags(); err != nil {
			return err
		}

		// Validate HTTPS port range
		if httpsPort < 1 || httpsPort > 65535 {
			return fmt.Errorf("invalid HTTPS port %d: must be between 1 and 65535", httpsPort)
		}
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
	// Call ResetCatalogCertificate with certificate paths
	return catalogPodman.ResetCatalogCertificate(catalogUtils.SanitizeFilePath(sslCertPath), catalogUtils.SanitizeFilePath(sslKeyPath))
}

func initConfigureCommonFlags() {
	common.ConfigureRuntimeFlag(configureCmd, &runtimeType)

	configureCmd.Flags().BoolVar(
		&resetPasswordFlag,
		"reset-password",
		false,
		"Reset the password for the admin user",
	)
}

func initConfigurePodmanFlags() {
	initConfigurePodmanDeployFlags()
	initConfigurePodmanResetFlags()
}

func initConfigurePodmanDeployFlags() {
	configureCmd.Flags().StringVar(
		&baseDir,
		"basedir",
		"",
		"Base directory for AI services data (models, caddy).\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --basedir /custom/path\n",
	)

	configureCmd.Flags().IntVar(
		&httpsPort,
		"https-port",
		defaultHTTPSPort,
		"Custom HTTPS port to expose the service endpoints externally.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --https-port 8443\n",
	)

	configureCmd.Flags().StringVar(
		&domainName,
		"domain-name",
		"",
		"Custom domain name for self-signed certificates.\n"+
			"If not provided, uses wildcard DNS format: <service>.<ip>.nip.io\n"+
			"If a custom SSL certificate/key pair is provided, the domain is extracted from the certificate and the --domain flag is ignored.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --domain-name example.com generates certs for *.example.com\n",
	)

	configureCmd.Flags().StringVar(
		&sslCertPath,
		"ssl-cert",
		"",
		"Path to user-provided SSL certificate (optional).\n"+
			"Must be used together with --ssl-key.\n"+
			"Certificate must contain wildcard SAN entry (e.g., *.example.com).\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --ssl-cert /path/to/cert.pem\n",
	)

	configureCmd.Flags().StringVar(
		&sslKeyPath,
		"ssl-key",
		"",
		"Path to user-provided SSL private key (optional).\n"+
			"Must be used together with --ssl-cert.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --ssl-key /path/to/key.pem\n",
	)
}

func initConfigurePodmanResetFlags() {
	configureCmd.Flags().BoolVar(
		&resetPodmanAuthFlag,
		"reset-podman-auth",
		false,
		"Reset podman authentication using the system's current auth.json.",
	)

	configureCmd.Flags().BoolVar(
		&resetCertificateFlag,
		"reset-certificate",
		false,
		"Reset the Caddy SSL certificates by loading new custom certificates.\n"+
			"Requires --ssl-cert and --ssl-key flags to specify the new certificate files.\n"+
			"This will reload the certificates in Caddy without restarting the pod.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example:\n"+
			"  ai-services catalog configure --runtime podman --reset-certificate --ssl-cert /path/to/cert.pem --ssl-key /path/to/key.pem\n",
	)
}

// buildFlagValidator registers every flag with its runtime scope.
func buildFlagValidator() *flagvalidator.FlagValidator {
	rt := vars.RuntimeFactory.GetRuntimeType()
	builder := flagvalidator.NewFlagValidatorBuilder(rt)

	// Common flags, valid for every runtime.
	builder.AddCommonFlag("reset-password", nil)

	// Podman-only flags.
	builder.
		AddPodmanFlag("basedir", nil).
		AddPodmanFlag("https-port", nil).
		AddPodmanFlag("domain-name", nil).
		AddPodmanFlag("ssl-cert", nil).
		AddPodmanFlag("ssl-key", nil).
		AddPodmanFlag("reset-podman-auth", nil).
		AddPodmanFlag("reset-certificate", nil)

	return builder.Build()
}

func runResetPassword() error {
	return catalogPodman.ResetCatalogPassword()
}

func runResetPodmanAuth() error {
	return catalogPodman.ResetPodmanAuth()
}

// Made with Bob
