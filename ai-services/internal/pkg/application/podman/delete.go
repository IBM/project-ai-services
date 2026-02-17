package podman

import (
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Delete removes an application and its associated resources.
func (p *PodmanApplication) Delete(opts types.DeleteOptions) error {
	logger.Warningln("yet to implement")

	return nil
}
