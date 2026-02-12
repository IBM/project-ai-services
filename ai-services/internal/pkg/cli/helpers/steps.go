package helpers

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

const (
	nextStepsMDFile = "next.md"
	nextStepsTitle  = "Next Steps"

	infoMDFile = "info.md"
	infoTitle  = "Info"
)

func PrintNextSteps(runtime runtime.Runtime, app, appTemplate string) error {
	params := map[string]string{"AppName": app}
	if err := renderStepsMarkdown(runtime, appTemplate, params, nextStepsMDFile, nextStepsTitle); err != nil {
		logger.Infof("Unable to load steps: %v\n", err)

		return nil
	}

	return nil
}

func PrintInfo(runtime runtime.Runtime, app, appTemplate string) error {
	params := map[string]string{"AppName": app}
	if err := renderStepsMarkdown(runtime, appTemplate, params, infoMDFile, infoTitle); err != nil {
		logger.Infof("Unable to load steps: %v\n", err)

		return nil
	}

	return nil
}

// populatePodValues -> populates the host values within the params.
func populateHostValues(params map[string]string, varsData *templates.Vars) error {
	for _, host := range varsData.Hosts {
		if host.Type == "ip" {
			// get the host IP
			hostIP, err := utils.GetHostIP()
			if err != nil {
				return fmt.Errorf("unable to fetch the host IP: %w", err)
			}
			params["HOST_IP"] = hostIP
		}
	}

	return nil
}

func populatePodInfo(runtime runtime.Runtime, params map[string]string, varsData *templates.Vars) error {
	for _, pod := range varsData.Pods {
		exists, err := runtime.PodExists(pod.Name)
		if err != nil {
			return fmt.Errorf("failed to check if pod exists: %w", err)
		}
		if !exists {
			// just print the msg
			logger.Infof("Pod with name: %s doesn't exist\n", pod.Name)

			continue
		}

		pInfo, err := runtime.InspectPod(pod.Name)
		if err != nil {
			return fmt.Errorf("failed to inspect Pod '%s': %w", pod.Name, err)
		}

		// Fetch specific Pod info based on the fetch value
		result, err := fetchDataSpecificInfo(pInfo, pod.Format, pod.Default)
		if err != nil {
			// just print the msg
			logger.Errorf("failed to fetch podInfo for pod: %s with err: %v\n", pod.Name, err)

			continue
		}

		// update the specific result to the params for given alias set
		params[pod.Alias] = result
	}

	return nil
}

func populateContainerInfo(runtime runtime.Runtime, params map[string]string, varsData *templates.Vars) error {
	for _, container := range varsData.Containers {
		exists, err := runtime.ContainerExists(container.Name)
		if err != nil {
			return fmt.Errorf("failed to check if container exists: %w", err)
		}
		if !exists {
			// just print the msg
			logger.Infof("Container with name: %s doesn't exist\n", container.Name)

			continue
		}

		cInfo, err := runtime.InspectContainer(container.Name)
		if err != nil {
			return fmt.Errorf("failed to inspect Container '%s': %w", container.Name, err)
		}

		// Fetch specific Container info based on the fetch value
		result, err := fetchDataSpecificInfo(cInfo, container.Format, container.Default)
		if err != nil {
			// just print the msg
			logger.Errorf("failed to fetch podInfo for pod: %s with err: %v\n", container.Name, err)

			continue
		}

		// update the specific result to the params for given alias set
		params[container.Alias] = result
	}

	return nil
}

// fetchDataSpecificInfo fetches the value from pod/container info based on the provided format.
// Data can be either the podInfo or the containerInfo.
// Format passed should support the podman --format notation without using '{{}}'.
func fetchDataSpecificInfo(data any, format string, defaultValue *string) (string, error) {
	// Converting format to template literal
	// Eg:- Template format: ".State" becomes "{{ .State }}"
	format = fmt.Sprintf("{{ %s }}", strings.TrimSpace(format))

	var result strings.Builder
	tmpl, err := template.New("format").Parse(format)
	if err != nil {
		return "", fmt.Errorf("parsing template for format %q: %w", format, err)
	}

	err = tmpl.Execute(&result, data)
	if err != nil {
		// if there is an error executing the template (Can occur for example if there is no port set)
		// if default value is passed, use the default value or else return error
		if defaultValue != nil {
			return strings.TrimSpace(*defaultValue), nil
		}

		return "", fmt.Errorf("executing template for format %q: %w", format, err)
	}

	return strings.TrimSpace(result.String()), nil
}

func renderStepsMarkdown(runtime runtime.Runtime, appTemplate string, params map[string]string, mdFile, title string) error {
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{})

	tmpls, err := tp.LoadMdFiles(appTemplate)
	if err != nil {
		return nil
	}

	tmpl, ok := tmpls[mdFile]
	if !ok {
		return nil
	}

	varsData, err := tp.LoadVarsFile(appTemplate, params)
	if err != nil {
		return fmt.Errorf("failed to load vars file: %w", err)
	}

	// populate the host values set in vars file
	if err := populateHostValues(params, varsData); err != nil {
		return fmt.Errorf("failed to populate host values: %w", err)
	}

	// populate the pod info set in vars file
	if err := populatePodInfo(runtime, params, varsData); err != nil {
		return fmt.Errorf("failed to populate pod values: %w", err)
	}

	// populate the container info set in vars file
	if err := populateContainerInfo(runtime, params, varsData); err != nil {
		return fmt.Errorf("failed to populate container values: %w", err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, params); err != nil {
		return fmt.Errorf("failed to execute info.md: %w", err)
	}

	logger.Infoln(title + ":")
	logger.Infoln("-------")
	logger.Infoln(rendered.String())

	return nil
}
