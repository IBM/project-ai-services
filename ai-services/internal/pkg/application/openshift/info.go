package openshift

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	cliTemplates "github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// Info displays detailed information about an application.
func (o *OpenshiftApplication) Info(opts types.InfoOptions) error {
	if opts.Legacy {
		return legacyInfo(opts, o.runtime)
	} else {
		return renderApplicationInfo(opts.Name)
	}
}

func legacyInfo(opts types.InfoOptions, runtime runtime.Runtime) error {
	// Step1: Do List pods and filter for given application name
	listFilters := map[string][]string{}
	if opts.Name != "" {
		listFilters["label"] = []string{fmt.Sprintf("ai-services.io/application=%s", opts.Name)}
	}

	pods, err := runtime.ListPods(listFilters)
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	// If there exists no pod for given application name, then fail saying application for given application name doesnt exist
	if len(pods) == 0 {
		logger.Infof("Application: '%s' does not exist.", opts.Name)

		return nil
	}

	logger.Infoln("Application Name: " + opts.Name)

	// Step2: From one of the pod, fetch and print the template and version label values

	appTemplate := pods[0].Labels[string(vars.TemplateLabel)]
	logger.Infoln("Application Template: " + appTemplate)

	version := pods[0].Labels[string(vars.VersionLabel)]
	logger.Infoln("Version: " + version)

	// Step3: Read and print the info.md file
	tp := cliTemplates.NewEmbedTemplateProvider(&assets.ApplicationFS)

	if err := helpers.PrintInfo(tp, runtime, opts.Name, appTemplate); err != nil {
		// not failing if overall info command, if we cannot display Info
		logger.Errorf("failed to display info: %v\n", err)

		return nil
	}

	return nil
}

func renderApplicationInfo(appName string) error {
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		return fmt.Errorf("failed to create application client: %w", err)
	}

	app, err := cliUtils.GetAppByName(appClient, appName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			logger.Warningf("Application: '%s' does not exist", appName)

			return nil
		}

		return err
	}

	application, err := appClient.GetApplication(app.ID)
	if err != nil {
		return fmt.Errorf("failed to get application: %w", err)
	}

	appPS, err := appClient.GetApplicationPS(app.ID)
	if err != nil {
		return fmt.Errorf("failed to get application pods: %w", err)
	}

	logger.Infoln("Application Name: " + application.Name)
	logger.Infoln("Application Template: " + application.CatalogID)
	logger.Infoln("Application Version: " + application.Version)

	return printServicesInfo(application.Services, appPS)
}

func printServicesInfo(services []catalogTypes.ApplicationService, appPS *catalogTypes.ApplicationPSResponse) error {
	catalogProvider, err := catalog.NewCatalogProvider()
	if err != nil {
		return fmt.Errorf("failed to create catalog provider: %w", err)
	}

	logger.Infoln("Info:")
	logger.Infoln("-------")
	logger.Infoln("Day N: ")

	for _, service := range services {
		params := map[string]string{}
		params["SERVICE_NAME"] = service.Type

		uiStatus, apiStatus := getPodStatus(appPS.Services, service.CatalogID)
		params["UI_STATUS"] = uiStatus
		params["API_STATUS"] = apiStatus

		for _, endpoint := range service.Endpoints {
			urlType, urlTypeOk := endpoint["type"].(string)
			url, urlOk := endpoint["url"].(string)
			if urlTypeOk && urlOk {
				params[strings.ToUpper(urlType)+"_URL"] = url
			}
		}

		tmpls, err := catalogProvider.LoadServicesMD(service.CatalogID)
		if err != nil {
			return fmt.Errorf("failed to load service md files: %w", err)
		}

		err = printInfo(tmpls, params)
		if err != nil {
			return fmt.Errorf("failed to load application info: %w", err)
		}
	}

	return nil
}

func getPodStatus(services []catalogTypes.Pod, catalogID string) (string, string) {
	uiStatus, apiStatus := "", ""

	for _, service := range services {
		if strings.HasPrefix(service.PodName, catalogID) && service.Status == catalogTypes.Running {
			// Check pod name to determine if it's UI or API
			if strings.Contains(service.PodName, "ui") {
				uiStatus = "running"
			} else if strings.Contains(service.PodName, "api") {
				apiStatus = "running"
			}
		}
	}

	return uiStatus, apiStatus
}

func printInfo(tmpls map[string]*template.Template, params map[string]string) error {
	tmpl, ok := tmpls["info.md"]
	if !ok {
		logger.Warningf("failed to find info.md template")

		return nil
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, params); err != nil {
		return fmt.Errorf("failed to execute info.md: %w", err)
	}
	value := rendered.String()
	value = strings.ReplaceAll(value, "Day N:\n", "")
	logger.Infoln(value)

	return nil
}
