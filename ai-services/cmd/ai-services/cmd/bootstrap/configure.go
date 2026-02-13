package bootstrap

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/spf13/cobra"
)

// configureCmd represents the validate subcommand of bootstrap.
func configureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "configure",
		Short:  "Configures the LPAR environment",
		Long:   `Configure and initialize the LPAR.`,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Once precheck passes, silence usage for any *later* internal errors.
			cmd.SilenceUsage = true

			logger.Infoln("Running bootstrap configuration...")

			// Create bootstrap instance based on runtime
			factory := bootstrap.NewFactoryFromEnv()
			bootstrapInstance, err := factory.Create()
			if err != nil {
				return fmt.Errorf("failed to create bootstrap instance: %w", err)
			}

			logger.Infof("Configuring for runtime: %s", factory.GetRuntimeType())
			if err := bootstrapInstance.Configure(); err != nil {
				return fmt.Errorf("bootstrap configuration failed: %w", err)
			}

			logger.Infof("Bootstrap configuration completed successfully.")

			return nil
		},
	}

	return cmd
}
