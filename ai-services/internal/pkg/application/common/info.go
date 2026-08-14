package common

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

// StatusFn derives UI and API status strings from a pod list for a given service catalogID.
// Each runtime provides its own implementation.
type StatusFn func(pods []catalogTypes.Pod, catalogID string) (uiStatus, apiStatus string)

// LegacyInfo implements the pre-catalog info path shared by both runtimes.
func LegacyInfo(opts types.InfoOptions, rt runtime.Runtime) error {
	listFilters := map[string][]string{}
	if opts.Name != "" {
		listFilters["label"] = []string{fmt.Sprintf("ai-services.io/application=%s", opts.Name)}
	}

	pods, err := rt.ListPods(listFilters)
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods) == 0 {
		logger.Infof("Application: '%s' does not exist.", opts.Name)

		return nil
	}

	logger.Infoln("Application Name: " + opts.Name)

	appTemplate := pods[0].Labels[string(vars.TemplateLabel)]
	logger.Infoln("Application Template: " + appTemplate)

	version := pods[0].Labels[string(vars.VersionLabel)]
	logger.Infoln("Version: " + version)

	tp := cliTemplates.NewEmbedTemplateProvider(&assets.ApplicationFS)

	if err := helpers.PrintInfo(tp, rt, opts.Name, appTemplate); err != nil {
		logger.Errorf("failed to display info: %v\n", err)
	}

	return nil
}

// RenderApplicationInfo implements the catalog-API-backed info path shared by both runtimes.
func RenderApplicationInfo(appName string, statusFn StatusFn) error {
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

	return printServicesInfo(application.Services, appPS, statusFn)
}

// printServicesInfo renders info.md for each service using the provided status function.
func printServicesInfo(services []catalogTypes.ApplicationService, appPS *catalogTypes.ApplicationPSResponse, statusFn StatusFn) error {
	catalogProvider, err := catalog.NewCatalogProvider()
	if err != nil {
		return fmt.Errorf("failed to create catalog provider: %w", err)
	}

	logger.Infoln("Info:")
	logger.Infoln("-------")
	logger.Infoln("Day N: ")

	for _, service := range services {
		params := map[string]string{
			"SERVICE_NAME": service.Type,
		}

		uiStatus, apiStatus := statusFn(appPS.Services, service.CatalogID)
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

		if err := printInfoMD(tmpls, params); err != nil {
			return fmt.Errorf("failed to load application info: %w", err)
		}
	}

	return nil
}

// printInfoMD executes the info.md template with the given params and prints the result.
func printInfoMD(tmpls map[string]*template.Template, params map[string]string) error {
	tmpl, ok := tmpls["info.md"]
	if !ok {
		logger.Warningf("failed to find info.md template")

		return nil
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, params); err != nil {
		return fmt.Errorf("failed to execute info.md: %w", err)
	}

	value := strings.ReplaceAll(rendered.String(), "Day N:\n", "")
	logger.Infoln(value)

	return nil
}
