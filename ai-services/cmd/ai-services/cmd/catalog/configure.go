package catalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure"
	catalogPodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure/podman"
	catalogConstant "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
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
	// Reset password flag for catalog configure command.
	resetPasswordFlag bool
)

const defaultHTTPSPort = 443
const catalogContainerName = "ai-services--catalog-backend"

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
			if resetPasswordFlag {
				// Reset Admin password
				if err := processResetPassword(); err != nil {
					return fmt.Errorf("failed to reset the password: %w", err)
				}
			} else {
				// Configure catalog service
				if err := runConfigure(); err != nil {
					return fmt.Errorf("failed to configure catalog service: %w", err)
				}
			}

			return nil
		},
	}

	configureConfigureFlags(cmd)
	configureBoolFlags(cmd)

	return cmd
}

// runConfigure executes the catalog configuration process.
func runConfigure() error {
	var aiServicesDir string
	var err error

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

	return configure.Run(configure.ConfigureOptions{
		BaseDir:     aiServicesDir,
		DomainName:  domainName,
		SSLCertPath: cleanCertPath,
		SSLKeyPath:  cleanKeyPath,
		HttpsPort:   httpsPort,
	})
}

func processResetPassword() error {
	rt, err := vars.RuntimeFactory.Create("")
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	podId, podEnv, err := getCatalogPodDetails(rt)
	if err != nil {
		return fmt.Errorf("failed to get existing catalog pod details: %w", err)
	}

	secretExists, err := rt.SecretExists(catalogConstant.CatalogSecretName)
	if err != nil {
		return fmt.Errorf("failed to check existing secrets: %w", err)
	}

	if secretExists {
		// Delete existing secret
		logger.Infof("Deleting existing catalog secret %s", catalogConstant.CatalogSecretName)
		err = rt.DeleteSecret(catalogConstant.CatalogSecretName)
		if err != nil {
			return fmt.Errorf("failed to delete existing catalog secret: %w", err)
		}
	}

	logger.Infof("Deleting existing catalog pod %s", podId)
	err = rt.DeletePod(podId, utils.BoolPtr(true))
	if err != nil {
		return fmt.Errorf("failed to delete existing catalog pod: %w", err)
	}

	setGlobalFlagValues(podEnv)

	return catalogPodman.DeployCatalog(context.Background(), baseDir, domainName, "", "", httpsPort)
}

func setGlobalFlagValues(podEnv map[string]string) {
	// Setting baseDir global variable
	if value, ok := podEnv["AI_SERVICES_BASE_DIR"]; ok {
		baseDir = value
	}

	// Setting domainName global variable
	if value, ok := podEnv["DOMAIN_SUFFIX"]; ok {
		domainName = value
	}

	// Setting httpsPort global variable
	if value, ok := podEnv["CADDY_HTTPS_PORT"]; ok {
		httpsPort, _ = strconv.Atoi(value)
	}
}

func getCatalogPodDetails(rt runtime.Runtime) (string, map[string]string, error) {
	// Build filter to find all pods using the catalog secret via label
	logger.Infof("Getting catalog pod details")
	filter := map[string][]string{
		"label": {fmt.Sprintf(
			"%s=%s",
			catalogConstant.CatalogSecretLabel,
			catalogConstant.CatalogSecretName,
		)},
	}

	// List all pods that reference the catalog secret
	pods, err := rt.ListPods(filter)
	if err != nil {
		return "", nil, fmt.Errorf("failed to list pods: %w", err)
	}
	if len(pods) == 0 {
		return "", nil, fmt.Errorf("no catalog pod found")
	}

	// Inspect catalog pod
	pod := pods[0]
	pInfo, err := rt.InspectPod(pod.ID)
	if err != nil {
		return "", nil, fmt.Errorf("failed to inspect pod %s: %w", pod.Name, err)
	}

	for _, container := range pInfo.Containers {
		if container.Name == catalogContainerName {
			// Inspect container for get hold of envs
			cInfo, err := rt.InspectContainer(container.ID)
			if err != nil {
				return "", nil, fmt.Errorf("failed to inspect container %s: %w", container.Name, err)
			}

			return pod.ID, cInfo.Env, nil
		}
	}

	return "", nil, fmt.Errorf("failed to find catalog container in pod %s", pod.Name)
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

func configureBoolFlags(cmd *cobra.Command) {
	cmd.Flags().BoolVar(
		&resetPasswordFlag,
		"reset-password",
		false,
		"Reset the password for the admin user",
	)
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
