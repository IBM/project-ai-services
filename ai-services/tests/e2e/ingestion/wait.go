package ingestion

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/config"
)

const (
	corePodsTimeout    = 20 * time.Minute
	ingestionTimeout   = 30 * time.Minute
	waitTickerInterval = 20 * time.Second
)

// WaitForAllPodsHealthy waits until required service pods
// (milvus, vllm-server, chat-bot) are Running and Healthy.
func WaitForAllPodsHealthy(
	ctx context.Context,
	cfg *config.Config,
	appName string,
) error {
	requiredPods := []string{
		//"--milvus",  --commented as currently switch to opensearch is in-progress
		"--vllm-server",
		"--chat-bot",
	}

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
			output, err := getAppStatusOutput(ctx, cfg, appName)
			if err != nil {
				continue
			}

			if areRequiredPodsHealthy(output, appName, requiredPods) {
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
) (string, error) {
	cmd := exec.CommandContext(
		ctx,
		cfg.AIServiceBin,
		"application",
		"ps",
		appName,
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
	requiredPods []string,
) bool {
	for _, suffix := range requiredPods {
		podName := appName + suffix
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

// WaitForIngestionLogs waits until ingestion completes successfully.
// It ONLY checks for the success log and ignores pod state.
func WaitForIngestionLogs(
	ctx context.Context,
	cfg *config.Config,
	appName string,
) (string, error) {
	podName := fmt.Sprintf("%s--ingest-docs", appName)

	ctx, cancel := context.WithTimeout(ctx, ingestionTimeout)
	defer cancel()

	ticker := time.NewTicker(waitTickerInterval)
	defer ticker.Stop()

	logger.Infof("[WAIT] Waiting for ingestion completion logs")

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()

		case <-ticker.C:
			cmd := exec.CommandContext(
				ctx,
				cfg.AIServiceBin,
				"application",
				"logs",
				appName,
				"--pod",
				podName,
			)

			out, err := cmd.CombinedOutput()
			if err != nil {
				continue
			}

			logs := string(out)

			if strings.Contains(logs, "Ingestion completed successfully") {
				logger.Infof("[WAIT] Ingestion completed successfully")

				return logs, nil
			}
		}
	}
}
