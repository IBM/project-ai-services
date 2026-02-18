package openshift

import (
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

func (o *OpenshiftApplication) Stop(opts types.StopOptions) error {
	logger.Warningln("not implemented")

	return nil
}
