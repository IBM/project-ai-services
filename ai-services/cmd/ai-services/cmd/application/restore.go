package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	restoreTarget   string
	restoreFilename string
	restoreAutoYes  bool
)

var restoreCmd = &cobra.Command{
	Use:   "restore [name]",
	Short: "Restore application data from a backup file",
	Long: `Restore application data from a tar.gz backup file.

Arguments:
  [name] : Application name (required)

Supported targets:
  - opensearch: Restore OpenSearch indices and data (Podman and OpenShift)
  - digitize:   Restore digitize metadata (jobs and documents) (Podman and OpenShift)

Note:
  - WARNING: Restore will overwrite existing data`,
	Example: `  For Podman:
  # Restore OpenSearch data with Podman
  ai-services application restore myapp --target opensearch --filename backup.tar.gz --runtime podman

  # Restore with automatic confirmation
  ai-services application restore myapp --target digitize --filename backup.tar.gz --runtime podman --yes

  For OpenShift:
  # Restore OpenSearch data with OpenShift
  ai-services application restore myapp --target opensearch --filename backup.tar.gz --runtime openshift

  # Restore digitize data with OpenShift
  ai-services application restore myapp --target digitize --filename digitize_backup.tar.gz --runtime openshift `,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		target := restoreTarget
		filename := restoreFilename

		// Once precheck passes, silence usage for any later internal errors
		cmd.SilenceUsage = true

		// Validate target
		validTargets := []string{"opensearch", "digitize"}
		isValid := false
		for _, t := range validTargets {
			if target == t {
				isValid = true

				break
			}
		}
		if !isValid {
			return fmt.Errorf("invalid target '%s'. Valid targets are: %s", target, strings.Join(validTargets, ", "))
		}

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
		ctx := context.Background()

		rt := vars.RuntimeFactory.GetRuntimeType()
		logger.Infof("Runtime: %s\n", rt)

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
			Target:     restoreTarget,
			BackupFile: absFilename,
			AutoYes:    restoreAutoYes,
		}

		// Execute restore using the application interface
		if err := app.Restore(ctx, opts); err != nil {
			return fmt.Errorf("failed to restore %s for application %s: %w", restoreTarget, applicationName, err)
		}

		logger.Infof("✓ Restore completed successfully for application %s\n", applicationName)

		return nil
	},
}

func init() {
	restoreCmd.Flags().StringVar(&restoreTarget, "target", "", "Target to restore (opensearch, digitize) (required)")
	restoreCmd.Flags().StringVar(&restoreFilename, "filename", "", "Path to the backup tar.gz file (required)")
	restoreCmd.Flags().BoolVarP(&restoreAutoYes, "yes", "y", false, "Automatically accept all confirmation prompts (default=false)")

	_ = restoreCmd.MarkFlagRequired("target")
	_ = restoreCmd.MarkFlagRequired("filename")
}

// Made with Bob
