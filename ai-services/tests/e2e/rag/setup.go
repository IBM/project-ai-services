package rag

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/tests/e2e/bootstrap"
	"github.com/project-ai-services/ai-services/tests/e2e/config"
)

var (
	ModelPath string
	Model     string
)

func init() {
	ModelPath, Model = bootstrap.GetLLMasJudgeModelDetails()
}

// runPodman runs a podman command with stdout/stderr attached to the process streams.
func runPodman(args ...string) error {
	cmd := exec.Command("podman", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

func startVLLMContainer(podName string, modelPath string) error {
	logger.Infof("Starting the VLLM Container")

	llmJudgePort, llmImage := bootstrap.GetLLMasJudgePodDetails()

	return runPodman(
		"run", "-d",
		"--name", podName,
		"-p", llmJudgePort+":"+llmJudgePort,
		"-v", modelPath+":/model:Z",
		"-e", "TORCHINDUCTOR_DISABLE=1",
		"-e", "TORCH_COMPILE=0",
		llmImage,
		"--model", "/model",
		"--tokenizer", "/model",
		"--dtype", "float32",
		"--enforce-eager",
		"--max-model-len", "4096",
		"--max-num-batched-tokens", "4096",
		"--served-model-name", Model,
	)
}

// judgeHealthy returns true when the vLLM judge /v1/models endpoint responds 200.
func judgeHealthy(port string) bool {
	url := fmt.Sprintf("http://localhost:%s/v1/models", port)
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return false
	}
	_ = resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

// judgeModelAlreadyDownloaded returns true when the model directory is non-empty.
func judgeModelAlreadyDownloaded() bool {
	modelDir := ModelPath + "/" + Model
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		return false
	}

	return len(entries) > 0
}

// DownloadJudgeModel logs in to the RH registry and downloads the judge model if not already present.
func DownloadJudgeModel(_ context.Context, _ *config.Config) error {
	if judgeModelAlreadyDownloaded() {
		logger.Infof("[JUDGE] Judge model already present at %s/%s — skipping download", ModelPath, Model)

		return nil
	}

	logger.Infof("[JUDGE] Logging in to RH registry and downloading judge model")

	url, uname, psswd := bootstrap.GetRHRegistryCreds()
	if loginErr := bootstrap.PodmanRegistryLogin(url, uname, psswd); loginErr != nil {
		logger.Errorf("error performing registry login %v", loginErr)

		return fmt.Errorf("error performing registry login: %w", loginErr)
	}
	logger.Infof("[JUDGE] RH Registry login completed")

	if modelErr := helpers.DownloadModel(Model, ModelPath); modelErr != nil {
		logger.Errorf("error downloading LLM as Judge model %v", modelErr)

		return fmt.Errorf("error downloading LLM as Judge model: %w", modelErr)
	}
	logger.Infof("[JUDGE] Judge model download completed")

	return nil
}

// removeJudgeContainers force-removes all containers whose names start with "vllm-judge-".
func removeJudgeContainers() {
	cmd := exec.Command("podman", "ps", "-a", "--format", "{{.Names}}", "--filter", "name=vllm-judge-")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return
	}

	for _, name := range strings.Fields(string(out)) {
		if strings.HasPrefix(name, "vllm-judge-") {
			logger.Infof("[JUDGE] Removing stale judge container: %s", name)
			_ = runPodman("rm", "-f", name)
		}
	}
}

// StartJudgeContainer starts the vLLM judge container and polls its /health endpoint until ready.
func StartJudgeContainer(_ context.Context, _ *config.Config, runID string) error {
	podName := "vllm-judge-" + runID
	llmJudgePort, _ := bootstrap.GetLLMasJudgePodDetails()

	removeJudgeContainers()

	if runErr := startVLLMContainer(podName, ModelPath+"/"+Model); runErr != nil {
		logger.Errorf("error running LLM as Judge container %v", runErr)

		return fmt.Errorf("error running LLM as Judge container: %w", runErr)
	}
	logger.Infof("[JUDGE] VLLM Judge container start triggered")

	const (
		pollInterval = 30 * time.Second
		maxWait      = 10 * time.Minute
	)

	deadline := time.Now().Add(maxWait)

	for {
		if judgeHealthy(llmJudgePort) {
			logger.Infof("[JUDGE] VLLM as Judge container started successfully")

			return nil
		}

		if time.Now().After(deadline) {
			break
		}

		logger.Infof("LLM server not started yet")
		time.Sleep(pollInterval)
	}

	logger.Errorf("polling attempts exhausted. VLLM Judge server was not started")

	return fmt.Errorf("polling attempts exhausted: VLLM Judge server was not started")
}

// SetupLLMAsJudge downloads the judge model then starts its container in sequence.
func SetupLLMAsJudge(ctx context.Context, cfg *config.Config, runID string) error {
	if err := DownloadJudgeModel(ctx, cfg); err != nil {
		return err
	}

	return StartJudgeContainer(ctx, cfg, runID)
}

func CleanupLLMAsJudge(runID string) error {
	logger.Infof("Stopping the VLLM Container")

	podName := "vllm-judge-" + runID

	if err := runPodman("stop", podName); err != nil {
		logger.Errorf("error stopping the container: %v", err)

		return fmt.Errorf("error stopping the container: %w", err)
	}

	if err := runPodman("rm", podName); err != nil {
		logger.Errorf("error removing the container: %v", err)

		return fmt.Errorf("error removing the container: %w", err)
	}

	return nil
}
