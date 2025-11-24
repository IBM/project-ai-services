package helpers

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/containers/podman/v5/pkg/domain/entities/types"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

func PrintNextSteps(runtime runtime.Runtime, app, appTemplate string) error {
	params := map[string]string{"AppName": app}
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{})

	stepsPath := appTemplate + "/steps"
	tmpls, err := tp.LoadMdFiles(stepsPath)
	if err != nil {
		// just printing and returning if the steps folder doesnt exist do not do anything
		logger.Infof("Unable to load steps: %v\n", err)
		return nil
	}

	if nextMd, ok := tmpls["next.md"]; ok {
		varsData, err := tp.LoadVarsFile(appTemplate, params)
		if err != nil {
			return fmt.Errorf("failed to load vars file: %w", err)
		}

		// populate the host values set in vars file
		if err := populateHostValues(params, varsData); err != nil {
			return fmt.Errorf("failed to populate host values: %w", err)
		}

		// populate the pod values set in vars file
		if err := populatePodValues(runtime, params, varsData); err != nil {
			return fmt.Errorf("failed to populate pod values: %w", err)
		}

		var rendered bytes.Buffer
		if err := nextMd.Execute(&rendered, params); err != nil {
			return fmt.Errorf("failed to execute next.md: %w", err)
		}

		logger.Infoln("Next Steps: ")
		logger.Infoln("-------")
		logger.Infoln(rendered.String())
	}

	return nil
}

func PrintInfo(runtime runtime.Runtime, app, appTemplate string) error {
	params := map[string]string{"AppName": app}
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{})

	stepsPath := appTemplate + "/steps"
	tmpls, err := tp.LoadMdFiles(stepsPath)
	if err != nil {
		// Returning if the steps folder doesnt exist do not do anything
		return nil
	}

	if nextMd, ok := tmpls["info.md"]; ok {
		varsData, err := tp.LoadVarsFile(appTemplate, params)
		if err != nil {
			return fmt.Errorf("failed to load vars file: %w", err)
		}

		// populate the host values set in vars file
		if err := populateHostValues(params, varsData); err != nil {
			return fmt.Errorf("failed to populate host values: %w", err)
		}

		// populate the pod values set in vars file
		if err := populatePodValues(runtime, params, varsData); err != nil {
			return fmt.Errorf("failed to populate pod values: %w", err)
		}

		var rendered bytes.Buffer
		if err := nextMd.Execute(&rendered, params); err != nil {
			return fmt.Errorf("failed to execute info.md: %w", err)
		}

		logger.Infoln("Info: ")
		logger.Infoln("-------")
		logger.Infoln(rendered.String())
	}

	return nil
}

// populatePodValues -> populates the host values within the params
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

// populatePodValues -> populates the pod values within the params
func populatePodValues(runtime runtime.Runtime, params map[string]string, varsData *templates.Vars) error {
	for _, pod := range varsData.Pods {
		if pod.Type == "port" {
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

			portMappings, err := fetchPodPortMapping(pInfo)
			if err != nil {
				return fmt.Errorf("failed to fetch PortMappings for pod '%s': %w", pod.Name, err)
			}

			// The Fetch value will contain the containerPort whose hostPort value needs to be fetched
			containerPort := strings.TrimSpace(pod.Fetch)
			hostPort := portMappings[containerPort]
			// Replace Alias with the hostPort value
			params[pod.Alias] = hostPort
		}
	}

	return nil
}

func fetchPodPortMapping(pInfo *types.PodInspectReport) (map[string]string, error) {
	portMappings := map[string]string{}

	if pInfo.InfraConfig == nil || pInfo.InfraConfig.PortBindings == nil {
		return portMappings, nil
	}

	for portKey, ports := range pInfo.InfraConfig.PortBindings {
		for _, port := range ports {
			// remove protocol
			containerPort := strings.Split(portKey, "/")[0]
			portMappings[containerPort] = port.HostPort
			// populating only the single host port value
			break
		}
	}

	return portMappings, nil
}
