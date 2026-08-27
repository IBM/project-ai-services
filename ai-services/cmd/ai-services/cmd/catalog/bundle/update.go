package bundle

import (
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// NewUpdateCmd implements: ai-services catalog bundle update <bundle_id> --file <path>.
func NewUpdateCmd() *cobra.Command {
	var filePath string

	cmd := &cobra.Command{
		Use:   "update <bundle_id>",
		Short: "Replace an existing catalog bundle",
		Long: `Replace the archive of an existing catalog bundle.

The bundle to replace is identified by its UUID, shown in the output of
'bundle list' or 'bundle create'. The catalog ID and type must match the
existing record; the version may differ and is read from the new archive.

The replacement is validated before any changes are made. If a service or
component is currently running from this bundle, the update is rejected until
those instances are deleted.

Note:
  Use 'bundle validate' to check an archive without modifying the catalog.`,
		Example: `  ai-services catalog bundle update 550e8400-e29b-41d4-a716-446655440000 --file ./my-service-v2.tar.gz`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			bundleID := args[0]

			c, err := client.NewBundleClient(ctx)
			if err != nil {
				return err
			}

			logger.InfofCtx(ctx, "Updating bundle %s from %s...\n", bundleID, filePath)

			resp, err := c.UpdateBundle(ctx, bundleID, filePath)
			if err != nil {
				return err
			}

			logger.InfofCtx(ctx, "✓ Bundle updated successfully (status: %s)\n", resp.Status)
			logger.InfofCtx(ctx, "  Catalog type: %s  |  Catalog ID: %s  |  Version: %s\n",
				resp.CatalogType, resp.CatalogID, resp.Version)
			logger.InfofCtx(ctx, "  Dir name:     %s-%s\n", resp.CatalogID, resp.Version)
			logger.InfolnCtx(ctx, "")

			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "Path to the replacement .tar.gz bundle archive (required)")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}
