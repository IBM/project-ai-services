package application

import (
	"fmt"

	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var infoCmd = &cobra.Command{
	Use:   "info [name]",
	Short: "Application info",
	Long: `Displays the information about the running application
		Arguments
		- [name]: Application name (Required)
	`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// fetch application name
		applicationName := args[0]

		runtimeClient, err := podman.NewPodmanClient()
		if err != nil {
			return fmt.Errorf("failed to connect to podman: %w", err)
		}

		// Step1: Do List pods and filter for given application name

		listFilters := map[string][]string{}
		if applicationName != "" {
			listFilters["label"] = []string{fmt.Sprintf("ai-services.io/application=%s", applicationName)}
		}

		resp, err := runtimeClient.ListPods(listFilters)
		if err != nil {
			return fmt.Errorf("failed to list pods: %w", err)
		}

		// TODO: Avoid doing the type assertion and importing types package from podman
		var pods []*types.ListPodsReport
		if val, ok := resp.([]*types.ListPodsReport); ok {
			pods = val
		}

		// If there exists no pod for given application name, then fail saying application for given application name doesnt exist
		if len(pods) == 0 {
			cmd.Printf("Application: '%s' does not exist.", applicationName)
			return nil
		}

		cmd.Println("Application Name: ", applicationName)

		// Step2: From one of the pod, fetch and print the template and version label values

		appTemplate := pods[0].Labels[string(vars.TemplateLabel)]
		cmd.Println("Application Template: ", appTemplate)

		version := pods[0].Labels[string(vars.VersionLabel)]
		cmd.Println("Version: ", version)

		// Step3: Read and print the info.md file

		if err := helpers.PrintInfo(runtimeClient, applicationName, appTemplate); err != nil {
			// not failing if overall info command, if we cannot display Info
			fmt.Printf("failed to display info: %v\n", err)
			return nil
		}

		return nil
	},
}
