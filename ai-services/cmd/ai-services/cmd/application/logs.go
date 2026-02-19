package application

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
)

var (
	podName           string
	containerNameOrID string
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show application pod logs",
	Long:  `Displays logs from an application pod`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if podName == "" {
			return fmt.Errorf("pod name must be specified using --pod flag")
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		rt := vars.RuntimeFactory.GetRuntimeType()

		// Create application instance using factory
		factory := application.NewFactory(rt)
		//nolint:godox
		// TODO: For OpenShift we need namespace param added if applicable
		app, err := factory.Create("")
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		opts := appTypes.LogsOptions{
			PodName:           podName,
			ContainerNameOrID: containerNameOrID,
		}

		return app.Logs(opts)
	},
}

func init() {
	logsCmd.Flags().StringVar(&podName, "pod", "", "Pod name to show logs from (required)")
	logsCmd.Flags().StringVar(&containerNameOrID, "container", "", "Container logs to show logs from (Optional)")
	_ = logsCmd.MarkFlagRequired("pod")
}
