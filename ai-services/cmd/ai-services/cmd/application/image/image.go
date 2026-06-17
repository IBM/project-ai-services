package image

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
)

var (
	templateName       string
	experimentalImages bool
)

var ImageCmd = &cobra.Command{
	Use:   "image",
	Short: "Manage application images",
	Long:  ``,
	Args:  cobra.MaximumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
}

// getCatalogImages is a helper that creates a catalog provider and collects images.
func getCatalogImages(templateID string) ([]string, error) {
	provider, err := catalog.NewCatalogProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to create catalog provider: %w", err)
	}

	images, err := provider.GetCatalogImages(context.Background(), templateID)
	if err != nil {
		return nil, err
	}

	return images, nil
}

func init() {
	ImageCmd.AddCommand(listCmd)
	ImageCmd.AddCommand(pullCmd)
	ImageCmd.PersistentFlags().StringVarP(&templateName, "template", "t", "", "Application template name (Required)")
	_ = ImageCmd.MarkPersistentFlagRequired("template")
	ImageCmd.PersistentFlags().BoolVar(&experimentalImages, "experimental", false, "Use experimental catalog-based image listing")
}
