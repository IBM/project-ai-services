package application

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

const (
	applicationTemplatesPath = "applications/"
)

var extraContainerReadinessTimeout = 5 * time.Minute

var templateName string

var createCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Deploys an application",
	Long: `Deploys an application with the provided application name based on the template
		Arguments
		- [name]: Application name (Required)
	`,
	Args: func(cmd *cobra.Command, args []string) error {
		if len(args) < 1 {
			return errors.New("you must provide an application name")
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		appName := args[0]

		cmd.Printf("Creating application '%s' using template '%s'\n", appName, templateName)

		// Fetch all the application Template names
		appTemplateNames, err := helpers.FetchApplicationTemplatesNames()
		if err != nil {
			return fmt.Errorf("failed to list templates: %w", err)
		}

		var appTemplateName string

		if index := fetchAppTemplateIndex(appTemplateNames, templateName); index == -1 {
			return errors.New("provided template name is wrong. Please provide a valid template name")
		} else {
			appTemplateName = appTemplateNames[index]
		}

		tmpls, err := helpers.LoadAllTemplates(applicationTemplatesPath + appTemplateName)
		if err != nil {
			return fmt.Errorf("failed to parse the templates: %w", err)
		}

		params := map[string]any{
			"AppName": appName,
		}

		// podman connectivity
		runtime, err := podman.NewPodmanClient()
		if err != nil {
			return fmt.Errorf("failed to connect to podman: %w", err)
		}

		// Loop through all pod templates, render and run kube play
		cmd.Printf("Total Pod Templates to be processed: %d\n", len(tmpls))
		for name, tmpl := range tmpls {
			cmd.Printf("Processing template: %s...\n", name)

			var rendered bytes.Buffer
			if err := tmpl.Execute(&rendered, params); err != nil {
				return fmt.Errorf("failed to execute template %s: %v", name, err)
			}

			// Wrap the bytes in a bytes.Reader
			reader := bytes.NewReader(rendered.Bytes())

			kubeReport, err := runtime.CreatePod(reader)
			if err != nil {
				return fmt.Errorf("failed pod creation: %w", err)
			}

			cmd.Printf("Successfully ran podman kube play for %s\n", name)

			for _, pod := range kubeReport.Pods {
				cmd.Printf("Performing Pod Readiness check...: %s\n", pod.ID)
				for _, containerID := range pod.Containers {
					cmd.Printf("Doing Container Readiness check...: %s\n", containerID)

					// getting the Start Period set for a container
					startPeriod, err := helpers.FetchContainerStartPeriod(runtime, containerID)
					if err != nil {
						return fmt.Errorf("fetching container start period failed: %w", err)
					}

					if startPeriod == -1 {
						cmd.Println("No container health check is set. Hence skipping readiness check")
						continue
					}

					// configure readiness timeout by appending start period with additional extra timeout
					readinessTimeout := startPeriod + extraContainerReadinessTimeout

					cmd.Printf("Setting the Waiting Readiness Timeout: %s\n", readinessTimeout)

					if err := helpers.WaitForContainerReadiness(runtime, containerID, readinessTimeout); err != nil {
						return fmt.Errorf("readiness check failed!: %w", err)
					}
					cmd.Printf("Container: %s is ready\n", containerID)
					cmd.Println("-------")
				}
				cmd.Printf("Pod: %s has been successfully deployed and ready!\n", pod.ID)
				cmd.Println("-------")
			}

			cmd.Println("-------\n-------")
		}

		return nil
	},
}

func init() {
	createCmd.Flags().StringVarP(&templateName, "template-name", "t", "", "Template name to use (required)")
	createCmd.MarkFlagRequired("template-name")
}

// fetchAppTemplateIndex -> Returns the index of app template if exists, otherwise -1
func fetchAppTemplateIndex(appTemplateNames []string, templateName string) int {
	appTemplateIndex := -1

	for index, appTemplateName := range appTemplateNames {
		if strings.EqualFold(appTemplateName, templateName) {
			appTemplateIndex = index
			break
		}
	}

	return appTemplateIndex
}
