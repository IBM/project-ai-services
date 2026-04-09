package bootstrap

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
)

var (
	// Runtime type flag for bootstrap command.
	runtimeType string
)

// BootstrapCmd represents the bootstrap command.
func BootstrapCmd() *cobra.Command {
	bootstrapCmd := &cobra.Command{
		Use:     "bootstrap",
		Short:   "Initializes AI Services infrastructure",
		Long:    bootstrapDescription(),
		Example: bootstrapExample(),
		Args:    cobra.NoArgs,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			// Initialize runtime factory based on flag
			rt := types.RuntimeType(runtimeType)
			if !rt.Valid() {
				return fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'openshift'). Please specify runtime using --runtime flag", runtimeType)
			}

			vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
			logger.Infof("Using runtime: %s\n", rt, logger.VerbosityLevelDebug)

			// Check if podman runtime is being used on unsupported platform
			return utils.CheckPodmanPlatformSupport(vars.RuntimeFactory.GetRuntimeType())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			rt := vars.RuntimeFactory.GetRuntimeType()
			// Create bootstrap instance based on runtime
			factory := bootstrap.NewBootstrapFactory(rt)
			bootstrapInstance, err := factory.Create()
			if err != nil {
				return fmt.Errorf("failed to create bootstrap instance: %w", err)
			}

			if configureErr := bootstrapInstance.Configure(); configureErr != nil {
				return fmt.Errorf("failed to run bootstrap configure: %w", configureErr)
			}

			if err := factory.Validate(nil); err != nil {
				return fmt.Errorf("failed to run bootstrap validate: %w", err)
			}

			if rt == types.RuntimeTypePodman {
				logger.Infoln("LPAR bootstrapped successfully")
				logger.Infoln("----------------------------------------------------------------------------")
			}

			return nil
		},
	}

	// Add runtime flag as required
	bootstrapCmd.PersistentFlags().StringVar(&runtimeType, "runtime", "", fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = bootstrapCmd.MarkPersistentFlagRequired("runtime")

	// subcommands
	bootstrapCmd.AddCommand(validateCmd())
	bootstrapCmd.AddCommand(configureCmd())

	return bootstrapCmd
}

func bootstrapExample() string {
	return `  # Validate the environment
  ai-services bootstrap validate

  # Validate the environment for openshift runtime
  ai-services bootstrap validate --runtime openshift

  # Configure the infrastructure
  ai-services bootstrap configure

  # Configure the infrastructure for openshift runtime
  ai-services bootstrap configure --runtime openshift

  # Get help on a specific subcommand
  ai-services bootstrap validate --help`
}

func bootstrapDescription() string {
	podmanList, openshiftList := generateValidationList()

	return fmt.Sprintf(`The bootstrap command configures and validates the environment needed
to run AI Services, ensuring prerequisites are met and initial configuration is completed.

Available subcommands:

Configure - Configure performs below actions
- For Podman:
 - Installs podman on host if not installed
 - Runs servicereport tool to configure required spyre cards
 - Initializes the AI Services infrastructure

- For OpenShift:
 - Applies required machine configs for Spyre operator
 - Installs required operators and operands
 - Creates and configures SpyreClusterPolicy
 - Creates DSCInitialization if it does not exist
 - Creates or updates DataScienceCluster with kserve enabled
 - Waits for all required components to become ready

Validate - Checks below system prerequisites:
- For Podman:
%s

- For OpenShift:
%s`, podmanList, openshiftList)
}
