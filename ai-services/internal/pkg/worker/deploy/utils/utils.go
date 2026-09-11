package utils

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workeruninstall "github.com/project-ai-services/ai-services/internal/pkg/worker/uninstall"
	workeruninstallutils "github.com/project-ai-services/ai-services/internal/pkg/worker/uninstall/utils"
)

const (
	// logPollInterval is how often we re-read container logs while waiting for
	// the worker to either connect successfully or emit an error.
	logPollInterval = 3 * time.Second
	// logPollTimeout is the maximum time we wait for the worker container to
	// emit a join-error log before declaring it healthy.
	logPollTimeout = 120 * time.Second
)

// CheckWorkerContainerLogs polls worker pod logs until the worker container
// either emits a join-error line or the poll window expires (treated as healthy).
// This poll is necessary because the container process needs a moment after
// startup to attempt the gRPC connection and write its output to the log buffer.
func CheckWorkerContainerLogs(ctx context.Context, rt runtime.Runtime) error {
	pods, err := rt.ListPods(ctx, map[string][]string{"label": {workerconstants.WorkerPodLabel}})
	if err != nil {
		return fmt.Errorf("failed to list worker pods: %w", err)
	}

	deadline := time.Now().Add(logPollTimeout)

	for _, pod := range pods {
		if err := pollPodLogs(ctx, rt, pod.Name, deadline); err != nil {
			return err
		}
	}

	return nil
}

// CheckLogsAndUninstall checks worker container logs and, on failure, runs Uninstall
// to clean up all worker components before returning the combined error.
func CheckLogsAndUninstall(ctx context.Context, rt runtime.Runtime) error {
	if err := CheckWorkerContainerLogs(ctx, rt); err != nil {
		logger.WarningfCtx(ctx, "Worker container logs indicate a failure (%v); uninstalling worker components...\n", err)

		if uninstallErr := workeruninstall.Uninstall(ctx, workeruninstallutils.UninstallOptions{
			RuntimeType: rt.Type(),
			AutoYes:     true,
		}); uninstallErr != nil {
			return fmt.Errorf("%w; worker uninstall: %w", err, uninstallErr)
		}

		return err
	}

	return nil
}

// pollPodLogs repeatedly fetches logs for a single pod until a join-success or
// join-error line is found, the deadline is reached, or the context is cancelled.
func pollPodLogs(ctx context.Context, rt runtime.Runtime, podName string, deadline time.Time) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		lines, err := rt.PodLogs(ctx, podName, false)
		if err != nil {
			return fmt.Errorf("failed to fetch logs for pod %s: %w", podName, err)
		}

		for _, line := range lines {
			if strings.Contains(line, workerconstants.WorkerJoinSuccess) {
				return nil
			}

			if strings.Contains(line, workerconstants.WorkerJoinErr) {
				return errors.New(line)
			}
		}

		if time.Now().After(deadline) {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(logPollInterval):
		}
	}
}
