package podman

import (
	"github.com/project-ai-services/ai-services/internal/pkg/application/common"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// List returns information about running applications.
func (p *PodmanApplication) List(opts appTypes.ListOptions) ([]appTypes.ApplicationInfo, error) {
	// filter and fetch pods based on appName
	pods, err := common.FetchFilteredPods(p.runtime, opts.ApplicationName)
	if err != nil {
		return nil, err
	}

	// if no pods are present and also if appName is provided then simply log and return
	if len(pods) == 0 && opts.ApplicationName != "" {
		logger.Infof("No Pods found for the given application name: %s", opts.ApplicationName)

		return nil, nil
	}

	// fetch the table writer object
	printer := utils.NewTableWriter()
	defer printer.CloseTableWriter()

	// set table headers and rows
	common.PopulateTable(p.runtime, opts, pods)

	return nil, nil
}
