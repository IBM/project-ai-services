package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/catalog/common"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/restore"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// NewRestoreCmd creates the "catalog restore" CLI command.
func NewRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore --filename <backup.tar.gz>",
		Short: "Restore catalog infrastructure data from a tar.gz backup archive",
		Long: `Restore all catalog infrastructure data from a backup archive created by "catalog backup".

The following data is restored from the archive:
  - PostgreSQL database
  - Caddy config related data - certificates, routes, config etc.

WARNING: This operation will overwrite the existing PostgreSQL database and Caddy
configuration. The Caddy pod is stopped during the operation and restarted afterwards.

The catalog service must be running when this command is executed.

Only the Podman runtime is supported currently.`,
		Example: `  # Restore from a backup file (interactive confirmation)
  ai-services catalog restore --runtime podman --filename /backups/catalog_backup_20240101.tar.gz

  # Restore without confirmation prompt
  ai-services catalog restore --runtime podman --filename /backups/catalog_backup_20240101.tar.gz --yes`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			if err := common.InitAndValidateRuntimeFlag(runtimeType); err != nil {
				return err
			}

			return validateCatalogRestoreFlags(catalogRestoreFilename)
		},
		RunE: runRestore,
	}

	common.ConfigureRuntimeFlag(cmd, &runtimeType)
	cmd.Flags().StringVar(
		&catalogRestoreFilename,
		"filename",
		"",
		"Path to the backup tar.gz file to restore from (required).\n"+
			"Example: --filename /backups/catalog_backup_20240101.tar.gz\n",
	)
	cmd.Flags().BoolVarP(&catalogRestoreAutoYes, "yes", "y", false, "Automatically accept all confirmation prompts (default=false)")
	_ = cmd.MarkFlagRequired("filename")

	return cmd
}

// catalogRestoreFilename is the required path to the backup archive.
var catalogRestoreFilename string

// catalogRestoreAutoYes skips the destructive-action confirmation prompt when true.
var catalogRestoreAutoYes bool

// runRestore is the RunE handler for the restore command.
func runRestore(_ *cobra.Command, _ []string) error {
	absFilename, err := filepath.Abs(catalogRestoreFilename)
	if err != nil {
		return fmt.Errorf("failed to resolve restore file path: %w", err)
	}

	if !catalogRestoreAutoYes {
		logger.Warningln("This operation will overwrite all existing catalog data (database, Caddy config)!")

		confirmed, err := utils.ConfirmAction("Are you sure you want to proceed with the restore? ")
		if err != nil {
			return fmt.Errorf("failed to get user confirmation: %w", err)
		}

		if !confirmed {
			logger.Infoln("Restore cancelled")

			return nil
		}
	}

	return restore.Run(vars.RuntimeFactory.GetRuntimeType(), absFilename)
}

// validateCatalogRestoreFlags validates the --filename flag for the restore command.
func validateCatalogRestoreFlags(filename string) error {
	if filename == "" {
		return fmt.Errorf("--filename is required")
	}

	if !strings.HasSuffix(filename, ".tar.gz") {
		return fmt.Errorf("backup filename must have a .tar.gz extension, got: %s", filename)
	}

	absFilename, err := filepath.Abs(filename)
	if err != nil {
		return fmt.Errorf("failed to resolve backup file path: %w", err)
	}

	if _, err := os.Stat(absFilename); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s", absFilename)
	}

	return nil
}

// Made with Bob
