package podman

import (
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Logs displays logs from an application pod.
func (p *PodmanApplication) Logs(opts types.LogsOptions) error {
	logger.Warningln("yet to implement")

	return nil
}
