package catalog

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/catalog/common"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/config"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// NewLogoutCmd returns the cobra command for logging out from the catalog API server.
func NewLogoutCmd() *cobra.Command {
	var (
		runtimeType string
	)
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out from the catalog API server",
		Long: `Invalidate the current session on the catalog API server and remove
the locally stored credentials.

Example:
  ai-services catalog logout --runtime podman`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return common.InitAndValidateRuntimeFlag(runtimeType)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Once precheck passes, silence usage for any *later* internal errors.
			cmd.SilenceUsage = true

			// Load credentials to check if the user is logged in.
			creds, err := config.Load()
			if err != nil {
				return err
			}

			logger.Infof("Logging out from %s...\n", creds.ServerURL)

			// Build a client from the stored credentials and call the server logout endpoint.
			// We use New() which also refreshes the token; if refresh fails we still
			// want to clean up local credentials, so we handle both paths.
			c, err := client.New()
			if err != nil {
				// Token may already be expired – still remove local credentials.
				logger.Warningf("could not reach server (%v). Removing local credentials anyway.\n", err)

				return config.Delete()
			}

			if err := c.Logout(); err != nil {
				return fmt.Errorf("logout failed: %w", err)
			}

			logger.Infoln("Logged out successfully.")

			return nil
		},
	}

	common.ConfigureRuntimeFlag(cmd, &runtimeType)

	return cmd
}

// Made with Bob
