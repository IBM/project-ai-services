package openshift

import (
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Delete removes an application and its associated resources.
func (o *OpenshiftApplication) Delete(opts types.DeleteOptions) error {
	logger.Warningln("not implemented")

	return nil
}
