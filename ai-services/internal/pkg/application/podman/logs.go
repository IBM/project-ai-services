package podman

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Logs displays logs from an application pod.
func (p *PodmanApplication) Logs(opts types.LogsOptions) error {
	ctx := context.Background()
	logger.Warningln("Press Ctrl+C to exit the logs and return to the terminal.")
	logger.Infof("Fetching logs for application pod: %s", opts.PodName)

	if opts.ContainerNameOrID == "" {
		if err := p.runtime.PodLogs(ctx, opts.PodName); err != nil {
			return fmt.Errorf("failed to fetch pod: %s logs; err: %w", opts.PodName, err)
		}

		return nil
	}

	// Fetch container logs
	exists, err := p.runtime.ContainerExists(ctx, opts.ContainerNameOrID)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("container %s doesn't exists", opts.ContainerNameOrID)
	}

	logger.Infof("Fetching logs for container: %s", opts.ContainerNameOrID)
	if err := p.runtime.ContainerLogs(ctx, opts.ContainerNameOrID); err != nil {
		return fmt.Errorf("failed to fetch container: %s logs; err: %w", opts.ContainerNameOrID, err)
	}

	return nil
}
