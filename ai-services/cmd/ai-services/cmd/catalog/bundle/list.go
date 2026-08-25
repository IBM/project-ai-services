package bundle

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// NewListCmd implements: ai-services catalog bundle list
func NewListCmd() *cobra.Command {
	var (
		page     int
		pageSize int
	)

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List all catalog bundles",
		Long: `List all custom bundles registered with the catalog.

Bundles are shown in a table ordered by registration time, most recent first.
Each row includes the bundle ID, catalog type, catalog ID, version, status,
and creation timestamp.

Use 'bundle info <bundle_id>' to view full details for a specific bundle.`,
		Example: `  ai-services catalog bundle list`,
		Args:    cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewBundleClient()
			if err != nil {
				return err
			}

			resp, err := c.ListBundles(page, pageSize)
			if err != nil {
				return err
			}

			return printBundleTable(resp)
		},
	}

	cmd.Flags().IntVar(&page, "page", 1, "Page number (1-indexed)")
	cmd.Flags().IntVar(&pageSize, "page-size", 20, "Number of items per page (1–100)")

	return cmd
}

const tablePadding = 3

func printBundleTable(resp *client.BundleListResponse) error {
	p := resp.Pagination

	if p.TotalItems == 0 {
		logger.Infoln("No bundles registered.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, tablePadding, ' ', 0)
	if _, err := fmt.Fprintln(w, "ID\tCATALOG TYPE\tCATALOG ID\tVERSION\tSTATUS\tCREATED AT"); err != nil {
		return err
	}

	for _, b := range resp.Bundles {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			b.ID, b.CatalogType, b.CatalogID, b.Version, b.Status,
			b.CreatedAt.Format("2006-01-02 15:04:05"),
		); err != nil {
			return err
		}
	}

	if err := w.Flush(); err != nil {
		return err
	}

	// Pagination summary — only shown when there is more than one page.
	if p.TotalPages > 1 {
		logger.Infof("\nPage %d of %d  (%d total)\n", p.Page, p.TotalPages, p.TotalItems)
		if p.HasNext {
			logger.Infof("Use --page %d to see the next page.\n", p.Page+1)
		}
	}

	return nil
}
