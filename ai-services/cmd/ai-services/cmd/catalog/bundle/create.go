package bundle

import (
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// NewCreateCmd implements: ai-services catalog bundle create --file <path>
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
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewBundleClient()
			if err != nil {
				return err
			}

			logger.Infof("Creating bundle from %s...\n", filePath)

			resp, err := c.CreateBundle(filePath)
			if err != nil {
				return err
			}

			logger.Infoln("✓ Bundle created successfully")
			logger.Infof("  ID:           %s\n", resp.ID)
			logger.Infof("  Catalog type: %s\n", resp.CatalogType)
			logger.Infof("  Catalog ID:   %s\n", resp.CatalogID)
			logger.Infof("  Dir name:     %s-%s\n", resp.CatalogID, resp.Version)
			logger.Infof("  Version:      %s\n", resp.Version)
			logger.Infof("  Status:       %s\n", resp.Status)
			if resp.SizeBytes != nil {
				logger.Infof("  Size:         %s\n", formatBytes(*resp.SizeBytes))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "Path to the .tar.gz bundle archive (required)")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}
