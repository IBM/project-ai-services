package catalog

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	appBootstrap "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/bootstrap"
	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/common"
	catalogOpenShift "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure/openshift"
	catalogPodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure/podman"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
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
	skipChecks  []string
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
	// WorkerGateway port — always active, defaults to 9090.
	workerGatewayPort int
	// Reset podman auth secret for catalog configure command.
	resetPodmanAuthFlag bool
	// Reset certificate flag for catalog configure command.
	resetCertificateFlag bool
	// Skip joining this machine as the Local worker.
	skipLocalWorkerFlag bool

	// openShift flags.
	timeout time.Duration
)

const (
	defaultHTTPSPort         = 443
	defaultWorkerGatewayPort = 9090
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
SSL/TLS certificate management, HTTPS port configuration, and credential/certificate reset capabilities.
Note: --workergateway-port is supported for podman runtime only (default 9090).`,
	Example: `  # Configure catalog service for podman
	 ai-services catalog configure --runtime podman

	 # Configure catalog service for OpenShift
	 ai-services catalog configure --runtime openshift

	 # Configure with a custom worker gateway port (podman only)
	 ai-services catalog configure --runtime podman --workergateway-port 9191

	 # Configure with custom HTTPS port
	 ai-services catalog configure --runtime podman --https-port 8443`,
	Args: cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		if err := common.InitAndValidateRuntimeFlag(runtimeType); err != nil {
			return err
		}

		// Reject runtime-scoped flags early.
		if err := buildCatalogFlagValidator().Validate(cmd); err != nil {
			return err
		}

		if resetPasswordFlag {
			return common.ValidateResetFlag(cmd, constants.ResetPasswordFlag)
		} else if resetPodmanAuthFlag {
			return common.ValidateResetFlag(cmd, constants.ResetPodmanAuthFlag)
		} else if resetCertificateFlag {
			return common.ValidateResetCertificateFlags(cmd, constants.ResetSSLCertFlag, sslCertPath, sslKeyPath, domainName)
		}

		return validateConfigureFlags()
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if resetPasswordFlag {
			return runResetPassword(ctx)
		} else if resetPodmanAuthFlag {
			return runResetPodmanAuth(ctx)
		} else if resetCertificateFlag {
			return runResetCertificate(ctx)
		}

		if err := common.DoBootstrapValidate(ctx, skipChecks); err != nil {
			return err
		}

		return runConfigure(ctx)
	},
}

// NewConfigureCmd returns the configure command for the catalog service.
func NewConfigureCmd() *cobra.Command {
	return configureCmd
}

func init() {
	initConfigureCommonFlags()
	initConfigurePodmanFlags()
	initConfigureOpenShiftFlags()
}

// buildCatalogFlagValidator registers every catalog configure flag with its runtime scope.
func buildCatalogFlagValidator() *flagvalidator.FlagValidator {
	return common.BuildFlagValidator(
		[]string{constants.ResetPasswordFlag, constants.SkipLocalWorkerFlag},
		[]string{constants.WorkerGatewayPortFlag, constants.BaseDirFlag, constants.HTTPSPortFlag, constants.DomainNameFlag, constants.SSLCertFlag, constants.SSLKeyFlag, constants.ResetPodmanAuthFlag, constants.ResetSSLCertFlag},
		[]string{constants.TimeoutFlag},
	)
}

// runConfigure executes the catalog configuration process.
func runConfigure(ctx context.Context) error {
	rt := vars.RuntimeFactory.GetRuntimeType()
	// Deploy catalog service based on runtime
	switch rt {
	case types.RuntimeTypePodman:
		// Resolve base directory: fall back to default when not provided.
		aiServicesDir, err := utils.ValidateBaseDir(baseDir)
		if err != nil {
			return fmt.Errorf("invalid base directory '%s': %w", baseDir, err)
		}

		// Create the models directory under the base dir.
		modelPath := filepath.Join(aiServicesDir, "models")
		if err := utils.CreateDir(modelPath); err != nil {
			return fmt.Errorf("failed to create model directory: %w", err)
		}

		opts := catalogUtils.PodmanConfigureOptions{
			BaseDir:           aiServicesDir,
			DomainName:        domainName,
			SSLCertPath:       catalogUtils.SanitizeFilePath(sslCertPath),
			SSLKeyPath:        catalogUtils.SanitizeFilePath(sslKeyPath),
			HttpsPort:         httpsPort,
			WorkerGatewayPort: workerGatewayPort,
			SkipLocalWorker:   skipLocalWorkerFlag,
		}

		return catalogPodman.DeployCatalog(ctx, opts)

	case types.RuntimeTypeOpenShift:
		opts := catalogUtils.OpenShiftConfigureOptions{
			Namespace:       catalogConstants.CatalogAppName,
			Timeout:         timeout,
			SkipLocalWorker: skipLocalWorkerFlag,
		}

		return catalogOpenShift.DeployCatalog(ctx, opts)
	default:
		return fmt.Errorf("unsupported runtime type: %s", rt)
	}
}

// validateConfigureFlags validates the configure command flags.
func validateConfigureFlags() error {
	// Podman-only validations
	if vars.RuntimeFactory.GetRuntimeType() == types.RuntimeTypePodman {
		if workerGatewayPort < 1 || workerGatewayPort > 65535 {
			return fmt.Errorf("invalid workergateway-port %d: must be between 1 and 65535", workerGatewayPort)
		}

		if err := utils.ValidateSSLFlags(sslCertPath, sslKeyPath, domainName); err != nil {
			return err
		}

		// Validate HTTPS port range
		if httpsPort < 1 || httpsPort > 65535 {
			return fmt.Errorf("invalid HTTPS port %d: must be between 1 and 65535", httpsPort)
		}
	}

	return nil
}

func runResetCertificate(ctx context.Context) error {
	// Call ResetCatalogCertificate with certificate paths
	return catalogPodman.ResetCatalogCertificate(ctx, catalogUtils.SanitizeFilePath(sslCertPath), catalogUtils.SanitizeFilePath(sslKeyPath))
}

func initConfigureCommonFlags() {
	common.ConfigureRuntimeFlag(configureCmd, &runtimeType)

	skipCheckDesc := appBootstrap.BuildSkipFlagDescription()
	configureCmd.Flags().StringSliceVar(&skipChecks, "skip-validation", []string{}, skipCheckDesc)

	configureCmd.Flags().BoolVar(
		&resetPasswordFlag,
		constants.ResetPasswordFlag,
		false,
		"Reset the password for the admin user",
	)

	configureCmd.Flags().BoolVar(
		&skipLocalWorkerFlag,
		constants.SkipLocalWorkerFlag,
		false,
		"Skip automatically joining this machine as the local worker after catalog deployment.",
	)
}

func initConfigurePodmanFlags() {
	common.ConfigurePodmanDeployFlags(configureCmd, &baseDir, &httpsPort, defaultHTTPSPort, &sslCertPath, &sslKeyPath, &domainName)
	common.ConfigurePodmanResetFlags(configureCmd, &resetPodmanAuthFlag, &resetCertificateFlag)

	configureCmd.Flags().IntVar(
		&workerGatewayPort,
		constants.WorkerGatewayPortFlag,
		defaultWorkerGatewayPort,
		"Port for the gRPC worker gateway that workers connect to.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --workergateway-port 9090\n",
	)
}

func runResetPassword(ctx context.Context) error {
	rt := vars.RuntimeFactory.GetRuntimeType()
	switch rt {
	case types.RuntimeTypePodman:
		return catalogPodman.ResetCatalogPassword(ctx)

	case types.RuntimeTypeOpenShift:
		return catalogOpenShift.ResetCatalogPassword(ctx)

	default:
		return fmt.Errorf("unsupported runtime: %s", rt)
	}
}

func runResetPodmanAuth(ctx context.Context) error {
	return catalogPodman.ResetPodmanAuth(ctx)
}

func initConfigureOpenShiftFlags() {
	configureCmd.Flags().DurationVar(
		&timeout,
		constants.TimeoutFlag,
		0,
		"Timeout for the operation (e.g. 10s, 2m, 1h).\n"+
			"Note: Supported for openshift runtime only.\n"+
			"Example: --timeout 30m\n",
	)
}

// Made with Bob
