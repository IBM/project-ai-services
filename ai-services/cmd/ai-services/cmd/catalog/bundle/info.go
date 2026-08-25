package bundle

import (
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// NewInfoCmd implements: ai-services catalog bundle info <bundle_id>.
func NewInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <bundle_id>",
		Short: "Show details for a catalog bundle",
		Long: `Display the full details of a catalog bundle.

The bundle is identified by its UUID, shown in the output of 'bundle list'
or 'bundle create'. The output includes the catalog ID, type, version,
on-disk directory name, status, size, creator, and registration timestamp.`,
		Example: `  ai-services catalog bundle info 550e8400-e29b-41d4-a716-446655440000`,
		Args:    cobra.ExactArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			bundleID := args[0]

			c, err := client.NewBundleClient()
			if err != nil {
				return err
			}

			b, err := c.GetBundle(bundleID)
			if err != nil {
				return err
			}

			logger.Infof("ID:           %s\n", b.ID)
			if b.Name != "" {
				logger.Infof("Name:         %s\n", b.Name)
			}
			logger.Infof("Catalog type: %s\n", b.CatalogType)
			logger.Infof("Catalog ID:   %s\n", b.CatalogID)
			logger.Infof("Dir name:     %s-%s\n", b.CatalogID, b.Version)
			logger.Infof("Version:      %s\n", b.Version)
			logger.Infof("Status:       %s\n", b.Status)
			if b.SizeBytes != nil {
				logger.Infof("Size:         %s\n", formatBytes(*b.SizeBytes))
			}
			if b.CreatedBy != "" {
				logger.Infof("Created by:   %s\n", b.CreatedBy)
			}
			logger.Infof("Created at:   %s\n", b.CreatedAt.Format("2006-01-02 15:04:05"))

			return nil
		},
	}

	return cmd
}
