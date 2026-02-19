package openshift

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// Info displays detailed information about an application.
func (o *OpenshiftApplication) Info(opts types.InfoOptions) error {
	// Step1: Do List pods and filter for given application name

	listFilters := map[string][]string{}
	if opts.Name != "" {
		listFilters["label"] = []string{fmt.Sprintf("ai-services.io/application=%s", opts.Name)}
	}

	pods, err := o.runtime.ListPods(listFilters)
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

	if err := helpers.PrintInfo(o.runtime, opts.Name, appTemplate); err != nil {
		// not failing if overall info command, if we cannot display Info
		logger.Errorf("failed to display info: %v\n", err)

		return nil
	}

	return nil
}
