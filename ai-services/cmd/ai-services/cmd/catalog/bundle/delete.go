package bundle

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// NewDeleteCmd implements: ai-services catalog bundle delete <bundle_id> [--yes].
func NewDeleteCmd() *cobra.Command {
	var skipConfirm bool

	cmd := &cobra.Command{
		Use:   "delete <bundle_id>",
		Short: "Delete a catalog bundle",
		Long: `Permanently remove a catalog bundle and deregister it from the catalog.

The bundle is identified by its UUID, shown in the output of 'bundle list'
or 'bundle create'. Both the stored archive and the catalog registration are
removed. The catalog is reloaded automatically after deletion.

If a service or component is currently running from this bundle, the deletion
is rejected until those instances are deleted.

Note:
  Existing deployed applications are not affected by deleting a bundle.`,
		Example: `  ai-services catalog bundle delete 550e8400-e29b-41d4-a716-446655440000
  ai-services catalog bundle delete 550e8400-e29b-41d4-a716-446655440000 --yes`,
		Args: cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			bundleID := args[0]

			if !skipConfirm {
				fmt.Printf("Delete bundle %s? [y/N] ", bundleID)
				var answer string
				_, _ = fmt.Scanln(&answer)
				if answer != "y" && answer != "Y" {
					logger.Infoln("Aborted.")

					return nil
				}
			}

			c, err := client.NewBundleClient()
			if err != nil {
				return err
			}

			if err := c.DeleteBundle(bundleID); err != nil {
				return err
			}

			logger.Infoln("✓ Bundle deleted.")

			return nil
		},
	}

	cmd.Flags().BoolVar(&skipConfirm, "yes", false, "Skip the confirmation prompt")

	return cmd
}
