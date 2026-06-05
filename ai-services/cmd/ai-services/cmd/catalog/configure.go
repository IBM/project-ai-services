package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/term"

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

const defaultHTTPSPort = 443

// NewConfigureCmd creates a new configure command for the catalog service.
func NewConfigureCmd() *cobra.Command {
	var (
		rawArgParams []string
		argParams    map[string]string
	)

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Configure the catalog service with initial configuration",
		Long: `Deploys the catalog service with the provided configuration.

Examples:
	 # Configure catalog service for podman
	 ai-services catalog configure --runtime podman

	 # Configure with custom UI port
	 ai-services catalog configure --runtime podman --params ui.port=8081`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			var err error
			argParams, err = validateConfigureFlags(rawArgParams)

			return err
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Prompt for admin password
			return runConfigure(argParams)
		},
	}

	configureConfigureFlags(cmd, &rawArgParams)

	return cmd
}

// runConfigure executes the catalog configuration process.
func runConfigure(argParams map[string]string) error {
	// Prompt for admin password
	adminPassword, err := promptForPassword()
	if err != nil {
		return fmt.Errorf("failed to read admin password: %w", err)
	}

	var aiServicesDir string

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
	cleanCertPath := filepath.Clean(sslCertPath)
	cleanKeyPath := filepath.Clean(sslKeyPath)

	return configure.Run(configure.ConfigureOptions{
		AdminPassword: adminPassword,
		Runtime:       vars.RuntimeFactory.GetRuntimeType(),
		BaseDir:       aiServicesDir,
		ArgParams:     argParams,
		DomainName:    domainName,
		SSLCertPath:   cleanCertPath,
		SSLKeyPath:    cleanKeyPath,
		HttpsPort:     httpsPort,
	})
}

// validateConfigureFlags validates the configure command flags and initializes runtime.
func validateConfigureFlags(rawArgParams []string) (map[string]string, error) {
	// Initialize runtime factory based on flag
	rt := types.RuntimeType(runtimeType)
	if !rt.Valid() {
		return nil, fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'openshift'). Please specify runtime using --runtime flag", runtimeType)
	}

	vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
	logger.Infof("Using runtime: %s\n", rt, logger.VerbosityLevelDebug)

	// Check if podman runtime is being used on unsupported platform
	if err := utils.CheckPodmanPlatformSupport(vars.RuntimeFactory.GetRuntimeType()); err != nil {
		return nil, err
	}

	// Validate SSL flags
	if err := validateSSLFlags(); err != nil {
		return nil, err
	}

	// Validate HTTPS port range
	if httpsPort < 1 || httpsPort > 65535 {
		return nil, fmt.Errorf("invalid HTTPS port %d: must be between 1 and 65535", httpsPort)
	}

	// Parse params if provided
	var argParams map[string]string
	if len(rawArgParams) > 0 {
		var err error
		argParams, err = utils.ParseKeyValues(rawArgParams)
		if err != nil {
			return nil, fmt.Errorf("invalid params format: %w", err)
		}
	}

	return argParams, nil
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
func configureConfigureFlags(cmd *cobra.Command, rawArgParams *[]string) {
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

	cmd.Flags().StringSliceVar(
		rawArgParams,
		"params",
		[]string{},
		"Inline parameters to configure the catalog service.\n\n"+
			"Format:\n"+
			"- Comma-separated key=value pairs\n"+
			"- Example: --params ui.port=8081,backend.port=8080\n\n"+
			"Available parameters:\n"+
			"- ui.port: Port for the catalog UI (default: random available port)\n"+
			"- backend.port: Port for the catalog backend API (default: random available port)\n",
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

// promptForPassword prompts the user to enter a password securely.
func promptForPassword() (string, error) {
	fmt.Print("Enter admin password: ")
	passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // Print newline after password input
	if err != nil {
		return "", err
	}

	password := string(passwordBytes)
	if password == "" {
		return "", fmt.Errorf("password cannot be empty")
	}

	// Prompt for confirmation
	fmt.Print("Confirm admin password: ")
	confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println() // Print newline after password input
	if err != nil {
		return "", err
	}

	confirm := string(confirmBytes)
	if password != confirm {
		return "", fmt.Errorf("passwords do not match")
	}

	return password, nil
}

// Made with Bob
