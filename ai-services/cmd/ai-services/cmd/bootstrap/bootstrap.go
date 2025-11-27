package bootstrap

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/root"
	"github.com/spf13/cobra"
)

// bootstrapCmd represents the bootstrap command
func BootstrapCmd() *cobra.Command {
	bootstrapCmd := &cobra.Command{
		Use:   "bootstrap",
		Short: "Initializes AI services infrastructure",
		Long: `Bootstrap and configure the infrastructure required for AI services.

The bootstrap command prepares and validates the environment needed
to run AI services on Power11 systems, ensuring prerequisites are met
and initial configuration is completed.

Available subcommands:
  validate   - Check system prerequisites and configuration
  configure  - Configure and initialize the AI services infrastructure`,
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
			return nil
		},
	}

	// subcommands
	bootstrapCmd.AddCommand(validateCmd())
	bootstrapCmd.AddCommand(configureCmd())

	return bootstrapCmd
}
