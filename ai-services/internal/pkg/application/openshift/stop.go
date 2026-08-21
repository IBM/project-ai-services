package openshift

import (
	"context"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Stop stops a running application.
func (o *OpenshiftApplication) Stop(_ context.Context, opts types.StopOptions) error {
	logger.Warningln("Not implemented")

	return nil
}
