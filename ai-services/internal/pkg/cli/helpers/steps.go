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

func PrintNextSteps(tp templates.Template, runtime runtime.Runtime, app, appTemplate string) error {
	params := map[string]string{"AppName": app}

	// Load values to make them available in templates
	values, err := tp.LoadValues(appTemplate, nil, nil)
	if err != nil {
		logger.Infof("Unable to load values: %v\n", err)
		values = make(map[string]interface{})
	}

	if err := renderStepsMarkdown(tp, runtime, appTemplate, params, values, nextStepsMDFile, nextStepsTitle); err != nil {
		logger.Infof("Unable to load steps: %v\n", err)

		return nil
	}

	return nil
}

func PrintInfo(tp templates.Template, runtime runtime.Runtime, app, appTemplate string) error {
	params := map[string]string{"AppName": app}

	// Load values to make them available in templates
	values, err := tp.LoadValues(appTemplate, nil, nil)
	if err != nil {
		logger.Infof("Unable to load values: %v\n", err)
		values = make(map[string]interface{})
	}

	// DEBUG: Print loaded values
	logger.Infof("DEBUG: Loaded values: %+v\n", values)

	// DEBUG: Check if instruct.apiKey exists
	if instructMap, ok := values["instruct"].(map[string]interface{}); ok {
		logger.Infof("DEBUG: instruct map found: %+v\n", instructMap)
		if apiKey, exists := instructMap["apiKey"]; exists {
			logger.Infof("DEBUG: instruct.apiKey value: '%v'\n", apiKey)
		} else {
			logger.Infof("DEBUG: instruct.apiKey key not found in instruct map\n")
		}
	} else {
		logger.Infof("DEBUG: instruct key not found or not a map in values\n")
	}

	if err := renderStepsMarkdown(tp, runtime, appTemplate, params, values, infoMDFile, infoTitle); err != nil {
		logger.Infof("Unable to load steps: %v\n", err)

		return nil
	}

	return nil
}

// populatePodValues -> populates the host values within the params.
func populateHostValues(runtime runtime.Runtime, params map[string]string, varsData *templates.Vars) error {
	for _, host := range varsData.Hosts {
		switch host.Type {
		case "ip":
			// get the host IP
			hostIP, err := utils.GetHostIP()
			if err != nil {
				return fmt.Errorf("unable to fetch the host IP: %w", err)
			}
			params["HOST_IP"] = hostIP
		case "route":
			route, err := runtime.ListRoutes()
			if err != nil {
				return fmt.Errorf("unable to fetch the route: %w", err)
			}

			// loop over each of the routes and populate the params
			for _, r := range route {
				// Replace hyphens with underscores to make valid Go template variable names
				sanitizedName := strings.ReplaceAll(r.Name, "-", "_")
				routeName := strings.ToUpper(fmt.Sprintf("%s_route", sanitizedName))
				params[routeName] = r.HostPort
			}
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

		// Sanitize alias by replacing hyphens with underscores for valid Go template variable names
		sanitizedAlias := strings.ReplaceAll(pod.Alias, "-", "_")
		// update the specific result to the params for given alias set
		params[sanitizedAlias] = result
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

		// Sanitize alias by replacing hyphens with underscores for valid Go template variable names
		sanitizedAlias := strings.ReplaceAll(container.Alias, "-", "_")
		// update the specific result to the params for given alias set
		params[sanitizedAlias] = result
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

func renderStepsMarkdown(tp templates.Template, runtime runtime.Runtime, appTemplate string, params map[string]string, values map[string]interface{}, mdFile, title string) error {
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
	if err := populateHostValues(runtime, params, varsData); err != nil {
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

	// Create template data combining params and values
	templateData := map[string]interface{}{
		"Values": values,
	}
	// Merge params into templateData
	for k, v := range params {
		templateData[k] = v
	}

	// DEBUG: Print template data structure
	logger.Infof("DEBUG: Values in templateData: %+v\n", templateData["Values"])

	// DEBUG: Check if Values.instruct.apiKey is accessible
	if valuesMap, ok := templateData["Values"].(map[string]interface{}); ok {
		if instructMap, ok := valuesMap["instruct"].(map[string]interface{}); ok {
			logger.Infof("DEBUG: templateData.Values.instruct.apiKey = '%v'\n", instructMap["apiKey"])
		} else {
			logger.Infof("DEBUG: instruct not found in Values or not a map\n")
		}
	} else {
		logger.Infof("DEBUG: Values not found in templateData or not a map\n")
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, templateData); err != nil {
		return fmt.Errorf("failed to execute info.md: %w", err)
	}

	logger.Infoln(title + ":")
	logger.Infoln("-------")
	logger.Infoln(rendered.String())
	logger.Infoln("") // Add Empty line after printing steps

	return nil
}
