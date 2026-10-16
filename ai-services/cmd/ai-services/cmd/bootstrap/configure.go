package bootstrap

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
)

// configureCmd represents the validate subcommand of bootstrap.
func configureCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "configure",
		Short:  "Configures the environment",
		Long:   `Configure and initialize the environment.`,
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Once precheck passes, silence usage for any *later* internal errors.
			cmd.SilenceUsage = true

			logger.Infoln("Running bootstrap configuration...")

			rt := vars.RuntimeFactory.GetRuntimeType()
			// Create bootstrap instance based on runtime
			factory := bootstrap.NewBootstrapFactory(rt)
			bootstrapInstance, err := factory.Create()
			if err != nil {
				return fmt.Errorf("failed to create bootstrap instance: %w", err)
			}

			// --upgrade is declared as a persistent flag on the parent bootstrap
			// command, so it is inherited here and accessible via upgradeOperators.
			bootstrap.SetUpgradeMode(rt, upgradeOperators)

			if err := bootstrapInstance.Configure(cmd.Context()); err != nil {
				return fmt.Errorf("bootstrap configuration failed: %w", err)
			}

			logger.Infof("Bootstrap configuration completed successfully.")

			return nil
		},
	}

	return cmd
}
