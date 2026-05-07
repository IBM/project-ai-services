package ingestion

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
	"github.com/project-ai-services/ai-services/tests/e2e/config"
)

const (
	corePodsTimeout    = 20 * time.Minute
	ingestionTimeout   = 30 * time.Minute
	waitTickerInterval = 20 * time.Second
)

// WaitForAllPodsHealthy waits until required service pods.
func WaitForAllPodsHealthy(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	appRuntime string,
) error {
	ctx, cancel := context.WithTimeout(ctx, corePodsTimeout)
	defer cancel()

	ticker := time.NewTicker(waitTickerInterval)
	defer ticker.Stop()

	logger.Infof("[WAIT] Waiting for core pods to be Running and Healthy")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			output, err := getAppStatusOutput(ctx, cfg, appName, appRuntime)
			if err != nil {
				continue
			}

			if areRequiredPodsHealthy(output, appName, appRuntime) {
				logger.Infof("[WAIT] All core pods are healthy")

				return nil
			}
		}
	}
}

// getAppStatusOutput fetches application pod status output.
func getAppStatusOutput(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	appRuntime string,
) (string, error) {
	cmd := exec.CommandContext(
		ctx,
		cfg.AIServiceBin,
		"application",
		"ps",
		appName,
		"--runtime",
		appRuntime,
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	return string(out), nil
}

// areRequiredPodsHealthy checks if all required pods are running and healthy.
func areRequiredPodsHealthy(
	output string,
	appName string,
	appRuntime string,
) bool {
	for _, suffix := range common.ExpectedPodSuffixes[appRuntime] {
		// Skip clean-docs and ingest-docs pods as they are not in running state
		if (suffix == "clean-docs") || (suffix == "ingest-docs") {
			continue
		}

		podName := appName + "--" + suffix
		podHealthy := false

		for _, line := range strings.Split(output, "\n") {
			if !strings.Contains(line, podName) {
				continue
			}

			if strings.Contains(line, "Running (healthy)") {
				podHealthy = true

				break
			}
		}

		if !podHealthy {
			return false
		}
	}

	return true
}
