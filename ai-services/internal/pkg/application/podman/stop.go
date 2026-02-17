package podman

import (
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Stop stops a running application.
func (p *PodmanApplication) Stop(opts types.StopOptions) error {
	logger.Warningln("yet to implement")

	return nil
}
