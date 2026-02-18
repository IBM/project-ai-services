package openshift

import (
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

func (o *OpenshiftApplication) List(opts types.ListOptions) ([]types.ApplicationInfo, error) {
	logger.Warningln("not implemented")

	return nil, nil
}
