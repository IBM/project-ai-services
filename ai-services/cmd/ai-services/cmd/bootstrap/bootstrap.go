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
		Short: "Bootstraps AI services infrastructure",
		Long: `Bootstrap and configure the AI services infrastructure.

The bootstrap command helps you set up and validate the environment
required to run AI services on Power11 systems.

Available subcommands:
  validate   - Validate system prerequisites and configuration
  configure  - Configure and initialize the AI services infrastructure`,
		Example: `  # Validate the environment
  aiservices bootstrap validate

  # Configure the infrastructure
  aiservices bootstrap configure

  # Get help on a specific subcommand
  aiservices bootstrap validate --help`,
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
