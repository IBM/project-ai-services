package application

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

var (
	restoreFilename string
	restoreAutoYes  bool
)

var restoreCmd = &cobra.Command{
	Use:   "restore [name]",
	Short: "Restore application data from a backup file",
	Long: `Restore application data from a tar.gz backup file.

Arguments:
  [name] : Application name (required)

The backup file is restored onto both:
  - OpenSearch indices and data (Podman and OpenShift)
  - Digitize metadata (jobs and documents) (Podman and OpenShift)

Older backup files that contain only one of the two are also accepted;
in that case only the part present in the file is restored.

Note:
  - WARNING: Restore will overwrite existing data`,
	Example: `  # Restore OpenSearch and digitize data from a backup file
  ai-services application restore myapp --filename backup.tar.gz

  # Restore with automatic confirmation
  ai-services application restore myapp --filename backup.tar.gz --yes`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		filename := restoreFilename

		// Once precheck passes, silence usage for any later internal errors
		cmd.SilenceUsage = true

		// Validate filename extension
		if !strings.HasSuffix(filename, ".tar.gz") {
			return fmt.Errorf("backup file must have .tar.gz extension, got: %s", filename)
		}

		// Check if file exists
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			return fmt.Errorf("backup file not found: %s", filename)
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		applicationName := args[0]
		ctx := cmd.Context()

		// Derive the runtime from the application's Worker record in the catalog,
		// or use --runtime directly if it was supplied.
		rt, err := resolveRuntimeForApp(ctx, applicationName, runtimeType)
		if err != nil {
			return err
		}

		// Get absolute path to backup file
		absFilename, err := filepath.Abs(restoreFilename)
		if err != nil {
			return fmt.Errorf("failed to get absolute path for backup file: %w", err)
		}

		// Display warning and get confirmation unless --yes flag is used
		if !restoreAutoYes {
			logger.Warningln("This operation will overwrite existing data!")

			confirmRestore, err := utils.ConfirmAction("Are you sure you want to proceed with the restore? ")
			if err != nil {
				return fmt.Errorf("failed to get user confirmation: %w", err)
			}
			if !confirmRestore {
				logger.Infoln("Restore cancelled")

				return nil
			}
		}

		// Create application instance using factory
		appFactory := application.NewFactory(rt)
		app, err := appFactory.Create(applicationName)
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		// Create restore options
		opts := appTypes.RestoreOptions{
			Name:       applicationName,
			BackupFile: absFilename,
			AutoYes:    restoreAutoYes,
		}

		// Execute restore using the application interface
		if err := app.Restore(ctx, opts); err != nil {
			return fmt.Errorf("failed to restore application %s: %w", applicationName, err)
		}

		logger.Infof("✓ Restore completed successfully for application %s\n", applicationName)

		return nil
	},
}

func init() {
	restoreCmd.Flags().StringVar(&restoreFilename, "filename", "", "Path to the backup tar.gz file (required)")
	restoreCmd.Flags().BoolVarP(&restoreAutoYes, "yes", "y", false, "Automatically accept all confirmation prompts (default=false)")

	_ = restoreCmd.MarkFlagRequired("filename")
}

// Made with Bob
