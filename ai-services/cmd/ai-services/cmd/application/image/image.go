package image

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	catalogclient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/config"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
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
// When a catalog server session is available it calls the API (which includes
// custom bundle services and architectures). Falls back to the local embedded
// catalog provider when no session exists (ErrNotLoggedIn).
// Architecture is tried first, then service.
func getCatalogImages(ctx context.Context, templateID string) ([]string, error) {
	appClient, err := catalogclient.NewApplicationClient(ctx)
	if err != nil {
		if !errors.Is(err, config.ErrNotLoggedIn) {
			return nil, fmt.Errorf("failed to create application client: %w", err)
		}

		// No active session — fall back to embedded-only local provider.
		logger.Warningln("Not logged in to catalog server, falling back to local embedded catalog (custom bundle items will not be included)")

		return getCatalogImagesLocal(ctx, templateID)
	}

	images, err := appClient.GetArchitectureImages(ctx, templateID)
	if err == nil {
		return images, nil
	}

	// Only retry as service when the server returned 404.
	var httpErr *catalogclient.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusNotFound {
		return nil, err
	}

	images, err = appClient.GetServiceImages(ctx, templateID)
	if err != nil {
		// Both lookups failed — the ID is not in the catalog.
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("template '%s' not found as a service or architecture", templateID)
		}

		return nil, err
	}

	return images, nil
}

// getCatalogImagesLocal collects images using the embedded-only local catalog provider.
// Used as a fallback when no catalog server session is available.
// Architecture is tried first, then service.
func getCatalogImagesLocal(ctx context.Context, templateID string) ([]string, error) {
	provider, err := catalog.NewCatalogProvider(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create catalog provider: %w", err)
	}

	images, err := provider.GetArchitectureImages(ctx, templateID)
	if err == nil {
		return images, nil
	}

	if !errors.Is(err, catalog.ErrCatalogItemNotFound) {
		return nil, err
	}

	images, err = provider.GetServiceImages(ctx, templateID)
	if err != nil {
		if errors.Is(err, catalog.ErrCatalogItemNotFound) {
			return nil, fmt.Errorf("template '%s' not found as a service or architecture", templateID)
		}

		return nil, err
	}

	return images, nil
}

func init() {
	ImageCmd.AddCommand(listCmd)
	ImageCmd.AddCommand(pullCmd)
	ImageCmd.PersistentFlags().StringVarP(&templateName, "template", "t", "", "Application template name (Required)")
	_ = ImageCmd.MarkPersistentFlagRequired("template")
	ImageCmd.PersistentFlags().BoolVar(&legacyImage, "legacy", false, "Use legacy application image implementation")
}
