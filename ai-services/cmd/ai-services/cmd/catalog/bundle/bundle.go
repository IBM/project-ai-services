// Package bundle implements the 'ai-services catalog bundle' subcommand group.
//
// Requires an active session — run 'ai-services catalog login' first.
// All subcommands call client.NewBundleClient() which loads stored credentials
// and returns ErrNotLoggedIn if no session exists.
package bundle

import (
	"github.com/spf13/cobra"
)

// NewBundleCmd returns the parent cobra command for catalog bundle management.
func NewBundleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bundle",
		Short: "Manage catalog bundles",
		Long: `Manage custom service and component bundles registered with the catalog.

Bundles extend the catalog at runtime with customer-authored assets packaged
as .tar.gz archives. Each bundle is validated, extracted, and hot-reloaded
into the running catalog with no restart required.

Use the subcommands below to create, update, delete, list, inspect, and
validate bundles.

Note:
  Requires an active session. Run 'ai-services catalog login' before
  using any bundle subcommand.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(NewCreateCmd())
	cmd.AddCommand(NewUpdateCmd())
	cmd.AddCommand(NewDeleteCmd())
	cmd.AddCommand(NewListCmd())
	cmd.AddCommand(NewInfoCmd())
	cmd.AddCommand(NewValidateCmd())

	return cmd
}
