package openshift

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/application/common"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// List returns information about running applications.
func (o *OpenshiftApplication) List(opts appTypes.ListOptions) ([]appTypes.ApplicationInfo, error) {
	if opts.ApplicationName == "" {
		return nil, fmt.Errorf("application name is required for openshift runtime")
	}

	// filter and fetch pods based on appName
	pods, err := common.FetchFilteredPods(o.runtime, opts.ApplicationName)
	if err != nil {
		return nil, err
	}

	// if no pods are present and also if appName is provided then simply log and return
	if len(pods) == 0 {
		logger.Infof("No Pods found for the given application name: %s", opts.ApplicationName)

		return nil, nil
	}

	// set table headers and rows
	common.PopulateTable(o.runtime, opts, pods)

	return nil, nil
}
