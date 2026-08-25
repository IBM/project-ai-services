package bundle

import (
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// NewValidateCmd implements: ai-services catalog bundle validate --file <path>.
func NewValidateCmd() *cobra.Command {
	var filePath string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a bundle archive without creating it",
		Long: `Check a bundle archive for correctness without registering it.

The archive is submitted to the catalog server for full validation, which
covers archive structure, metadata, values schema, and template files.
Nothing is stored and the catalog is not modified.

The command exits with a non-zero status if validation fails.`,
		Example: `  ai-services catalog bundle validate --file ./my-service-bundle.tar.gz`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewBundleClient()
			if err != nil {
				return err
			}

			logger.Infof("Validating bundle from %s...\n", filePath)

			result, err := c.ValidateBundle(filePath)
			if err != nil {
				logger.Errorf("✗ Bundle validation failed: %v\n", err)

				return err
			}

			logger.Infoln("✓ Bundle is valid")
			logger.Infof("  Catalog type: %s\n", result.CatalogType)
			if result.ComponentType != "" {
				logger.Infof("  Component type: %s\n", result.ComponentType)
			}
			logger.Infof("  Catalog ID:   %s\n", result.CatalogID)
			logger.Infof("  Dir name:     %s-%s\n", result.CatalogID, result.Version)
			logger.Infof("  Version:      %s\n", result.Version)
			if result.Name != "" {
				logger.Infof("  Name:         %s\n", result.Name)
			}

			logger.Infoln("")

			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "Path to the .tar.gz bundle archive (required)")
	_ = cmd.MarkFlagRequired("file")

	return cmd
}
