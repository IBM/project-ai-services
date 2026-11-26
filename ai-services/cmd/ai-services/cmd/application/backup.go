package application

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
)

var backupFilename string

var backupCmd = &cobra.Command{
	Use:   "backup [name]",
	Short: "Backup application data to a file",
	Long: `Backup application data to a single tar.gz backup file.

Arguments:
  [name] : Application name (required)

The backup contains both:
  - OpenSearch indices and data (Podman and OpenShift)
  - Digitize metadata (jobs and documents) (Podman and OpenShift)`,
	Example: `  # Backup the application (OpenSearch + digitize) into one archive
  ai-services application backup myapp

  # Backup with custom filename
  ai-services application backup myapp --filename mybackup.tar.gz`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Validate filename extension if provided
		if backupFilename != "" && !strings.HasSuffix(backupFilename, ".tar.gz") {
			return fmt.Errorf("backup file must have .tar.gz extension, got: %s", backupFilename)
		}

		// Check if file already exists (if filename is provided)
		if backupFilename != "" {
			absFilename, err := filepath.Abs(backupFilename)
			if err != nil {
				return fmt.Errorf("failed to get absolute path for backup file: %w", err)
			}
			if _, err := os.Stat(absFilename); err == nil {
				return fmt.Errorf("backup file already exists: %s", absFilename)
			}
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		applicationName := args[0]
		ctx := cmd.Context()

		// Once precheck passes, silence usage for any later internal errors
		cmd.SilenceUsage = true

		// Derive the runtime from the application's Worker record in the catalog,
		// or use --runtime directly if it was supplied.
		rt, err := resolveRuntimeForApp(ctx, applicationName, runtimeType)
		if err != nil {
			return err
		}

		// Get absolute path to backup file if provided
		var absFilename string
		if backupFilename != "" {
			absFilename, err = filepath.Abs(backupFilename)
			if err != nil {
				return fmt.Errorf("failed to get absolute path for backup file: %w", err)
			}
		}

		// Create application instance using factory
		appFactory := application.NewFactory(rt)
		app, err := appFactory.Create(applicationName)
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		// Create backup options
		opts := appTypes.BackupOptions{
			Name:       applicationName,
			BackupFile: absFilename, // Can be empty for auto-generation
		}

		// Execute backup using the application interface
		return app.Backup(ctx, opts)
	},
}

func init() {
	backupCmd.Flags().StringVar(&backupFilename, "filename", "", "Path to save the backup tar.gz file (optional, auto-generated if not specified)")
}

// Made with Bob
