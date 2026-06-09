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
	restoreTarget   string
	restoreFilename string
)

var restoreCmd = &cobra.Command{
	Use:   "restore [name]",
	Short: "Restore application data from a backup file",
	Long: `Restore application data from a tar.gz backup file.

Arguments:
  [name] : Application name (required)

Flags:
  --target   : Target to restore (opensearch, digitize) (required)
  --filename : Path to the backup tar.gz file (required)

Supported targets:
  - opensearch: Restore OpenSearch indices and data (Podman only)
  - digitize:   Not yet implemented

Note:
  - OpenSearch restore is currently only supported for Podman runtime
  - OpenSearch password is automatically retrieved from the application's secret

Examples:
  # Restore OpenSearch data with Podman
  ai-services application restore myapp --target opensearch --filename backup.tar.gz --runtime podman
`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		target := restoreTarget
		filename := restoreFilename

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

		// Once precheck passes, silence usage for any later internal errors
		cmd.SilenceUsage = true

		rt := vars.RuntimeFactory.GetRuntimeType()
		logger.Infof("Runtime: %s\n", rt, 0)

		// Check if OpenShift runtime is being used
		if rt == "openshift" {
			return fmt.Errorf("restore is not yet supported for OpenShift runtime")
		}

		// Get absolute path to backup file
		absFilename, err := filepath.Abs(restoreFilename)
		if err != nil {
			return fmt.Errorf("failed to get absolute path for backup file: %w", err)
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
		}

		// Execute restore using the application interface
		return app.Restore(ctx, opts)
	},
}

func init() {
	restoreCmd.Flags().StringVar(&restoreTarget, "target", "", "Target to restore (opensearch, digitize) (required)")
	restoreCmd.Flags().StringVar(&restoreFilename, "filename", "", "Path to the backup tar.gz file (required)")

	_ = restoreCmd.MarkFlagRequired("target")
	_ = restoreCmd.MarkFlagRequired("filename")
}

// Made with Bob
