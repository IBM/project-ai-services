package image

import (
	"context"
	"errors"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
)

var (
	templateName string
	legacyImage  bool
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

// getCatalogImages returns container images for the given template ID.
// It calls the service images API first; if the ID is not a service (404)
// it retries against the architecture images API.
func getCatalogImages(ctx context.Context, templateID string) ([]string, error) {
	appClient, err := client.NewApplicationClient(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := appClient.GetServiceImages(ctx, templateID)
	if err == nil {
		return resp.Images, nil
	}

	// Only fall through to architecture when the server returned 404.
	var httpErr *client.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusNotFound {
		return nil, err
	}

	resp, err = appClient.GetArchitectureImages(ctx, templateID)
	if err != nil {
		return nil, err
	}

	return resp.Images, nil
}

func init() {
	ImageCmd.AddCommand(listCmd)
	ImageCmd.AddCommand(pullCmd)
	ImageCmd.PersistentFlags().StringVarP(&templateName, "template", "t", "", "Application template name (Required)")
	_ = ImageCmd.MarkPersistentFlagRequired("template")
	ImageCmd.PersistentFlags().BoolVar(&legacyImage, "legacy", false, "Use legacy application image implementation")
}
