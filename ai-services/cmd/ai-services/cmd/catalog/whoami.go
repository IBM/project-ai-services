package catalog

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/catalog/common"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// NewWhoamiCmd returns the cobra command that prints the currently authenticated user.
func NewWhoamiCmd() *cobra.Command {
	var (
		runtimeType string
	)
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the currently authenticated user",
		Long: `Retrieve and display information about the user that is currently
logged in to the catalog API server.`,
		Example: `  # Show currently authenticated user for podman runtime
  ai-services catalog whoami --runtime podman

Note:
  - Requires prior authentication via 'ai-services catalog login'`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return common.InitAndValidateRuntimeFlag(runtimeType)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Once precheck passes, silence usage for any *later* internal errors.
			cmd.SilenceUsage = true

			c, err := client.New()
			if err != nil {
				return err
			}

			info, err := c.Me()
			if err != nil {
				return fmt.Errorf("get user info: %w", err)
			}

			logger.Infof("Server  : %s\n", c.ServerURL())
			logger.Infof("User ID : %s\n", info.ID)
			logger.Infof("Username: %s\n", info.Username)
			logger.Infof("Name    : %s\n", info.Name)

			return nil
		},
	}

	common.ConfigureRuntimeFlag(cmd, &runtimeType)

	return cmd
}

// Made with Bob
