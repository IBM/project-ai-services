package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
)

// CheckWorkerContainerLogs lists all worker pods, fetches a snapshot of current
// logs for each pod, and returns an error if the line "failed to join the worker"
// is found in any of them.
func CheckWorkerContainerLogs(ctx context.Context, rt runtime.Runtime) error {
	pods, err := rt.ListPods(ctx, map[string][]string{"label": {workerconstants.WorkerPodLabel}})
	if err != nil {
		return fmt.Errorf("failed to list worker pods: %w", err)
	}

	fmt.Println("PODS: ", pods)
	for _, pod := range pods {

		fmt.Println("Pods Container", pod.Containers)
		lines, err := rt.PodLogs(ctx, pod.Name, false)
		fmt.Println("Lines: ", lines)
		if err != nil {
			logger.WarningfCtx(ctx, "failed to fetch logs for pod %s: %v\n", pod.Name, err)

			continue
		}

		for _, line := range lines {
			if strings.Contains(line, workerconstants.WorkerJoinErr) {
				return fmt.Errorf("%s", line)
			}
		}
	}

	return nil
}
