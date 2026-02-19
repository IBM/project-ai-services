package openshift

import (
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// List returns information about running applications.
func (o *OpenshiftApplication) List(opts types.ListOptions) ([]types.ApplicationInfo, error) {
	logger.Warningln("not implemented")

	return nil, nil
}
