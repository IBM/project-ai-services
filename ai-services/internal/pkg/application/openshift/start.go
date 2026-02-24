package openshift

import (
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Start starts a stopped application.
func (o *OpenshiftApplication) Start(opts types.StartOptions) error {
	logger.Warningln("not implemented")

	return nil
}
