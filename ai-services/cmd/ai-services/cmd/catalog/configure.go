package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
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
)

const (
	defaultHTTPSPort = 443
)

// NewConfigureCmd creates a new configure command for the catalog service.
func NewConfigureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Configure the catalog service with initial configuration",
		Long: `Deploys the catalog service with the provided configuration.

Examples:
	 # Configure catalog service for podman
	 ai-services catalog configure --runtime podman

	 # Configure with custom HTTPS port
	 ai-services catalog configure --runtime podman --https-port 8443`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			return validateConfigureFlags()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigure()
		},
	}

	configureConfigureFlags(cmd)

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

	logger.Infof("Using base directory: %s\n", aiServicesDir, logger.VerbosityLevelDebug)

	// create model directory
	modelPath := filepath.Join(aiServicesDir, "models")
	err = utils.CreateDir(modelPath)
	if err != nil {
		return fmt.Errorf("failed to create model directory: %w", err)
	}

	// Sanitize SSL certificate paths to prevent path traversal attacks
	// Only clean if paths are provided (filepath.Clean("") returns ".")
	cleanCertPath := ""
	cleanKeyPath := ""
	if sslCertPath != "" {
		cleanCertPath = filepath.Clean(sslCertPath)
	}
	if sslKeyPath != "" {
		cleanKeyPath = filepath.Clean(sslKeyPath)
	}

	return configure.Run(vars.RuntimeFactory.GetRuntimeType(), aiServicesDir, domainName, cleanCertPath, cleanKeyPath, httpsPort)
}

// validateConfigureFlags validates the configure command flags and initializes runtime.
func validateConfigureFlags() error {
	// Initialize runtime factory based on flag
	rt := types.RuntimeType(runtimeType)
	if !rt.Valid() {
		return fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'openshift'). Please specify runtime using --runtime flag", runtimeType)
	}

	vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
	logger.Infof("Using runtime: %s\n", rt, logger.VerbosityLevelDebug)

	// Check if podman runtime is being used on unsupported platform
	if err := utils.CheckPodmanPlatformSupport(vars.RuntimeFactory.GetRuntimeType()); err != nil {
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

// configureConfigureFlags configures the flags for the configure command.
func configureConfigureFlags(cmd *cobra.Command) {
	// Add runtime flag as required
	cmd.Flags().StringVarP(&runtimeType, "runtime", "r", "", fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = cmd.MarkFlagRequired("runtime")

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

// Made with Bob
