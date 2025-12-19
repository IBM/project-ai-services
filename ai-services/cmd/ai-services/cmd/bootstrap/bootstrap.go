package bootstrap

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/root"
	"github.com/spf13/cobra"
)

// bootstrapCmd represents the bootstrap command
func BootstrapCmd() *cobra.Command {
	bootstrapCmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Initializes AI Services infrastructure",
		Long: `Bootstrap and configure the infrastructure required for AI Services.

The bootstrap command configures and validates the environment needed
to run AI Services on Power11 systems, ensuring prerequisites are met
and initial configuration is completed.

Available subcommands:
  configure - Configure performs below actions
  • Installs podman on host if not installed
  • Runs servicereport tool to configure required spyre cards
  • Initializes the AI Services infrastructure

  validate - Checks below system prerequisites
  • Root user
  • Power11 server
  • RHEL OS
  • LPAR affinity
  • Spyre cards availability
  • ServiceReport validation`,
		Example: `  # Validate the environment
  ai-services bootstrap validate

  # Configure the infrastructure
  ai-services bootstrap configure

  # Get help on a specific subcommand
  ai-services bootstrap validate --help`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return root.NewRootRule().Verify()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			logger.Infof("Configuring the LPAR")
			if configureErr := RunConfigureCmd(); configureErr != nil {
				return fmt.Errorf("failed to bootstrap the LPAR: %w", configureErr)
			}

			logger.Infof("Validating LPAR")
			if validateErr := RunValidateCmd(nil); validateErr != nil {
				return fmt.Errorf("failed to bootstrap the LPAR: %w", validateErr)
			}

			logger.Infoln("LPAR boostrapped successfully")
			logger.Infoln("----------------------------------------------------------------------------")
			style := lipgloss.NewStyle().Foreground(lipgloss.Color("#32BD27"))
			message := style.Render("Re-login to the shell to reflect necessary permissions assigned to vfio cards")
			logger.Infoln(message)

			return nil
		},
	}

	// subcommands
	bootstrapCmd.AddCommand(validateCmd())
	bootstrapCmd.AddCommand(configureCmd())

	return bootstrapCmd
}
