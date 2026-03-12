package bootstrap

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
)

// BootstrapCmd represents the bootstrap command.
func BootstrapCmd() *cobra.Command {
	bootstrapCmd := &cobra.Command{
		Use:     "bootstrap",
		Short:   "Initializes AI Services infrastructure",
		Long:    bootstrapDescription(),
		Example: bootstrapExample(),
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
				return fmt.Errorf("failed to bootstrap the LPAR: %w", configureErr)
			}

			if err := factory.Validate(nil); err != nil {
				return fmt.Errorf("failed to bootstrap the LPAR: %w", err)
			}

			if rt == types.RuntimeTypePodman {
				logger.Infoln("LPAR bootstrapped successfully")
				logger.Infoln("----------------------------------------------------------------------------")
				style := lipgloss.NewStyle().Foreground(lipgloss.Color("#32BD27"))
				message := style.Render("Re-login to the shell to reflect necessary permissions assigned to vfio cards")
				logger.Infoln(message)
			}

			return nil
		},
	}

	// subcommands
	bootstrapCmd.AddCommand(validateCmd())
	bootstrapCmd.AddCommand(configureCmd())

	return bootstrapCmd
}

func bootstrapExample() string {
	return `  # Validate the environment
  ai-services bootstrap validate

  # Configure the infrastructure
  ai-services bootstrap configure

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
   - Installs machine config, and dependant operators
   - Installs and configures SpyreClusterPolicy	

Validate - Checks below system prerequisites:
- For Podman:
%s

- For Openshift:
%s`, podmanList, openshiftList)
}
