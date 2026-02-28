//go:build catalog_api
// +build catalog_api

package catalog

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
)

// NewWhoamiCmd returns the cobra command that prints the currently authenticated user.
func NewWhoamiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the currently authenticated user",
		Long: `Retrieve and display information about the user that is currently
logged in to the catalog API server.

Example:
		ai-services catalog whoami`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.New()
			if err != nil {
				return err
			}

			info, err := c.Me()
			if err != nil {
				return fmt.Errorf("get user info: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Server  : %s\n", c.ServerURL())
			fmt.Fprintf(cmd.OutOrStdout(), "User ID : %s\n", info.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Username: %s\n", info.Username)
			fmt.Fprintf(cmd.OutOrStdout(), "Name    : %s\n", info.Name)

			return nil
		},
	}

	return cmd
}

// Made with Bob
