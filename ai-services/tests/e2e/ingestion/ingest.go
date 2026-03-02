package ingestion

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/common"
	"github.com/project-ai-services/ai-services/tests/e2e/config"
)

// PrepareDocs copies ingestion PDFs to the app ingestion directory.
func PrepareDocs(appName string, fileName string) error {
	// Resolve current folder: tests/e2e/ingestion.
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("unable to resolve ingestion directory")
	}
	srcDir := filepath.Join(filepath.Dir(filename), "docs", fileName)

	dstDir := filepath.Join(
		"/var/lib/ai-services/applications",
		appName,
		"docs",
	)

	if err := common.EnsureDir(dstDir); err != nil {
		return fmt.Errorf("failed to create docs dir: %w", err)
	}

	dstDir = filepath.Join(
		dstDir, fileName,
	)

	return common.CopyFile(srcDir, dstDir)
}

// StartIngestion waits for the vLLM pod to be ready and then starts the ingestion pod.
func StartIngestion(
	ctx context.Context,
	cfg *config.Config,
	appName string,
	completionStr string,
) error {
	// Wait for vLLM pod to be ready.
	if err := WaitForAllPodsHealthy(ctx, cfg, appName); err != nil {
		return err
	}

	// Start ingestion pod.
	podName := fmt.Sprintf("%s--ingest-docs", appName)

	args := []string{
		"application", "start",
		appName,
		"--pod", podName,
		"--yes",
	}

	logger.Infof("[CLI] Running: %s %s", cfg.AIServiceBin, strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, cfg.AIServiceBin, args...)
	out, err := cmd.CombinedOutput()
	output := string(out)
	logger.Infof("[CLI] Output: %s", output)

	if err != nil {
		return fmt.Errorf("failed to start ingestion pod: %w\n%s", err, output)
	}

	// Wait for ingestion to complete.
	if _, err := WaitForIngestionLogs(ctx, cfg, appName, completionStr); err != nil {
		return err
	}

	return nil
}

// CleanDocsFolder removes the documents from the application documents folder.
func CleanDocsFolder(appName string) error {
	docsDir := filepath.Join(
		"/var/lib/ai-services/applications",
		appName,
		"docs",
	)

	if err := common.EnsureDir(docsDir); err != nil {
		return fmt.Errorf("docs directory does not exist: %w", err)
	}

	return common.RemoveDirContents(docsDir)
}
