package catalog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/catalog/common"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/backup"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	// backupFilename is an optional output path for the catalog backup archive.
	catalogBackupFilename string
)

// NewBackupCmd creates the "catalog backup" CLI command.
func NewBackupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Back up catalog infrastructure data to a tar.gz archive",
		Long: `Back up all catalog infrastructure data required to restore the catalog service.

The following data is included in the backup archive:
  - PostgreSQL database dump
  - Caddy config related data - certificates, routes, config etc.

The catalog service must be running when this command is executed.`,
		Example: `  # Backup with auto-generated filename
  ai-services catalog backup --runtime podman

  # Backup to a specific file
  ai-services catalog backup --runtime podman --filename /backups/catalog_20240101.tar.gz`,
		Args: cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true

			if err := common.InitAndValidateRuntimeFlag(runtimeType); err != nil {
				return err
			}

			return validateCatalogBackupFlags(catalogBackupFilename)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			var absFilename string

			if catalogBackupFilename != "" {
				var err error

				absFilename, err = filepath.Abs(catalogBackupFilename)
				if err != nil {
					return fmt.Errorf("failed to resolve backup file path: %w", err)
				}
			}

			return backup.Run(vars.RuntimeFactory.GetRuntimeType(), absFilename)
		},
	}

	common.ConfigureRuntimeFlag(cmd, &runtimeType)
	cmd.Flags().StringVar(
		&catalogBackupFilename,
		"filename",
		"",
		"Path for the backup tar.gz file (optional; auto-generated as catalog_backup_<timestamp>.tar.gz if not specified).\n"+
			"Example: --filename /backups/catalog_20240101.tar.gz\n",
	)

	return cmd
}

// validateCatalogBackupFlags validates the optional --filename flag.
func validateCatalogBackupFlags(filename string) error {
	if filename == "" {
		return nil
	}

	if !strings.HasSuffix(filename, ".tar.gz") {
		return fmt.Errorf("backup filename must have a .tar.gz extension, got: %s", filename)
	}

	absFilename, err := filepath.Abs(filename)
	if err != nil {
		return fmt.Errorf("failed to resolve backup file path: %w", err)
	}

	if _, err := os.Stat(absFilename); err == nil {
		return fmt.Errorf("backup file already exists: %s", absFilename)
	}

	return nil
}

// Made with Bob
