package catalog

import "github.com/spf13/cobra"

// CatalogCmd returns the cobra command for managing the AI Services catalog service, including subcommands for the API server.
func CatalogCmd() *cobra.Command {
	catalogCMD := &cobra.Command{
		Use:   "catalog",
		Short: "Manage AI Services catalog service",
		Long: `catalog service provides APIs to manage AI Services catalog, including listing available services,
deploying services, and managing service metadata`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	catalogCMD.AddCommand(NewAPIServerCmd())
	catalogCMD.AddCommand(NewConfigureCmd())
	catalogCMD.AddCommand(NewHashpwCmd())
	catalogCMD.AddCommand(NewLoginCmd())
	catalogCMD.AddCommand(NewLogoutCmd())
	catalogCMD.AddCommand(NewWhoamiCmd())

	return catalogCMD
}

// Made with Bob
