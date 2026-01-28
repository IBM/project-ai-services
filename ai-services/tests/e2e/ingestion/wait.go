package ingestion

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/tests/e2e/config"
)

// WaitForCorePodsHealthy waits until only the required service pods
// (milvus, vllm-server, chat-bot) are Running and Healthy.
func WaitForAllPodsHealthy(
	ctx context.Context,
	cfg *config.Config,
	appName string,
) error {

	requiredPods := []string{
		"--milvus",
		"--vllm-server",
		"--chat-bot",
	}

	timeout := time.After(20 * time.Minute)
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	fmt.Printf("[WAIT] Waiting for core pods to be Running and Healthy\n")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-timeout:
			return fmt.Errorf("timed out waiting for core pods to be healthy")

		case <-ticker.C:
			cmd := exec.CommandContext(
				ctx,
				cfg.AIServiceBin,
				"application", "ps",
				appName,
			)

			out, err := cmd.CombinedOutput()
			if err != nil {
				continue
			}

			output := string(out)
			healthyCount := 0

			for _, suffix := range requiredPods {
				podName := appName + suffix
				foundHealthy := false

				for _, line := range strings.Split(output, "\n") {
					if strings.Contains(line, podName) &&
						strings.Contains(line, "Running (healthy)") {

						foundHealthy = true
						break
					}
				}

				if foundHealthy {
					healthyCount++
				}
			}

			if healthyCount == len(requiredPods) {
				fmt.Printf("[WAIT] All core pods are healthy\n")
				return nil
			}
		}
	}
}

// WaitForIngestionLogs waits until ingestion completes successfully.
// It ONLY checks for the success log and ignores pod state.
func WaitForIngestionLogs(
	ctx context.Context,
	cfg *config.Config,
	appName string,
) (string, error) {

	podName := fmt.Sprintf("%s--ingest-docs", appName)
	timeout := time.After(30 * time.Minute)
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	fmt.Printf("[WAIT] Waiting for ingestion completion logs\n")

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()

		case <-timeout:
			return "", fmt.Errorf(
				"timed out waiting for ingestion completion for app %s",
				appName,
			)

		case <-ticker.C:
			cmd := exec.CommandContext(
				ctx,
				cfg.AIServiceBin,
				"application", "logs",
				appName,
				"--pod", podName,
			)

			out, err := cmd.CombinedOutput()
			if err != nil {
				continue
			}

			logs := string(out)

			if strings.Contains(logs, "Ingestion completed successfully") {
				fmt.Printf("[WAIT] Ingestion completed successfully\n")
				return logs, nil
			}
		}
	}
}
