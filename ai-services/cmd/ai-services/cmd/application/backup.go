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
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	backupTarget   string
	backupFilename string
)

var backupCmd = &cobra.Command{
	Use:   "backup [name]",
	Short: "Backup application data to a file",
	Long: `Backup application data to a tar.gz backup file.

Arguments:
  [name] : Application name (required)

Flags:
  --target   : Target to backup (opensearch, digitize) (required)
  --filename : Path to save the backup tar.gz file (optional)
               If not specified, a filename will be auto-generated with timestamp

Supported targets:
  - opensearch: Backup OpenSearch indices and data (Podman and OpenShift)
  - digitize:   Backup digitize metadata (jobs and documents) (Podman and OpenShift)
Examples:
  # Backup OpenSearch data with Podman (auto-generated filename)
  ai-services application backup myapp --target opensearch --runtime podman

  # Backup digitize data with OpenShift
  ai-services application backup myapp --target digitize --runtime openshift

  # Backup digitize data with custom filename
  ai-services application backup myapp --target digitize --filename mybackup.tar.gz --runtime podman
`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		target := backupTarget

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
		ctx := context.Background()

		// Once precheck passes, silence usage for any later internal errors
		cmd.SilenceUsage = true

		rt := vars.RuntimeFactory.GetRuntimeType()
		logger.Infof("Runtime: %s\n", rt)

		// Get absolute path to backup file if provided
		var absFilename string
		if backupFilename != "" {
			var err error
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
			Target:     backupTarget,
			BackupFile: absFilename, // Can be empty for auto-generation
		}

		// Execute backup using the application interface
		return app.Backup(ctx, opts)
	},
}

func init() {
	backupCmd.Flags().StringVar(&backupTarget, "target", "", "Target to backup (opensearch, digitize) (required)")
	backupCmd.Flags().StringVar(&backupFilename, "filename", "", "Path to save the backup tar.gz file (optional, auto-generated if not specified)")

	_ = backupCmd.MarkFlagRequired("target")
}

// Made with Bob
