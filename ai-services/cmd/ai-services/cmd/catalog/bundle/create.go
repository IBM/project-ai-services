package bundle

import (
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// NewCreateCmd implements: ai-services catalog bundle create --file <path>.
func NewCreateCmd() *cobra.Command {
	var filePath string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new catalog bundle",
		Long: `Register a new custom service or component bundle with the catalog.

The archive must be a gzip-compressed tar (.tar.gz) containing a metadata.yaml
at its root. The catalog ID, type, and version are read from that file — no
additional flags are required.

The bundle is validated, extracted, and made active before the command returns.
On success, the assigned bundle ID and metadata are printed.

Note:
  Use 'bundle validate' to check an archive without registering it.`,
		Example: `  ai-services catalog bundle create --file ./my-service-bundle.tar.gz`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			c, err := client.NewBundleClient(ctx)
			if err != nil {
				return err
			}

			logger.InfofCtx(ctx, "Creating bundle from %s...\n", filePath)

			resp, err := c.CreateBundle(ctx, filePath)
			if err != nil {
				return err
			}

			logger.InfolnCtx(ctx, "✓ Bundle created successfully")
			logger.InfofCtx(ctx, "  ID:           %s\n", resp.ID)
			logger.InfofCtx(ctx, "  Catalog type: %s\n", resp.CatalogType)
			logger.InfofCtx(ctx, "  Catalog ID:   %s\n", resp.CatalogID)
			logger.InfofCtx(ctx, "  Dir name:     %s-%s\n", resp.CatalogID, resp.Version)
			logger.InfofCtx(ctx, "  Version:      %s\n", resp.Version)
			logger.InfofCtx(ctx, "  Status:       %s\n", resp.Status)
			if resp.SizeBytes != nil {
				logger.InfofCtx(ctx, "  Size:         %s\n", utils.FormatBytes(*resp.SizeBytes))
			}

			logger.InfolnCtx(ctx, "")

			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "Path to the .tar.gz bundle archive (required)")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}
