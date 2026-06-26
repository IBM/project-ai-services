package catalog

import "github.com/spf13/cobra"

// CatalogCmd returns the cobra command for managing the AI Services catalog service, including subcommands for the API server.
func CatalogCmd() *cobra.Command {
	catalogCMD := &cobra.Command{
		Use:   "catalog",
		Short: "Manage the AI Services catalog",
		Long: `The catalog service offers APIs for managing the AI Services catalog, enabling you to list available services,
deploy them, and handle service metadata`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	catalogCMD.AddCommand(NewAPIServerCmd())
	catalogCMD.AddCommand(NewConfigureCmd())
	catalogCMD.AddCommand(NewUninstallCmd())
	catalogCMD.AddCommand(NewHashpwCmd())
	catalogCMD.AddCommand(NewLoginCmd())
	catalogCMD.AddCommand(NewLogoutCmd())
	catalogCMD.AddCommand(NewWhoamiCmd())
	catalogCMD.AddCommand(NewMigrateCmd())
	catalogCMD.AddCommand(NewInfoCmd())

	return catalogCMD
}

// Made with Bob
