package openshift

import (
	"context"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Start starts a stopped application.
func (o *OpenshiftApplication) Start(_ context.Context, opts types.StartOptions) error {
	logger.Warningln("Not supported for openshift runtime")

	return nil
}
