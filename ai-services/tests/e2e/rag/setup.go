package rag

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
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

func hasLLMServerStarted(podName string) (isStarted bool) {
	grep := exec.Command("grep", "gRPC Server started at")
	podmanLogs := exec.Command("podman", "logs", podName)

	pipe, _ := podmanLogs.StdoutPipe()
	defer func() {
		_ = pipe.Close()
	}()

	grep.Stdin = pipe
	err := podmanLogs.Start()
	if err != nil {
		logger.Errorf("Error starting vllm judge pod logs %v", err)

		return false
	}

	// Run and get the output of grep.
	out, err := grep.Output()
	if exitError, ok := err.(*exec.ExitError); ok {
		// The command failed, check the exit code
		if status, ok := exitError.Sys().(syscall.WaitStatus); ok {
			if status.ExitStatus() == 1 {
				logger.Infof("LLM server not started yet")

				return false
			}
		}
		logger.Errorf("Error fetching vllm judge pod logs %v", err)

		return false
	}

	output := string(out)

	return output != ""
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

// StartJudgeContainer starts the vLLM judge container and polls until ready; must run after the main LLM is up to avoid GPU contention.
func StartJudgeContainer(_ context.Context, _ *config.Config, runID string) error {
	podName := "vllm-judge-" + runID
	if runErr := startVLLMContainer(podName, ModelPath+"/"+Model); runErr != nil {
		logger.Errorf("error running LLM as Judge container %v", runErr)

		return fmt.Errorf("error running LLM as Judge container: %w", runErr)
	}
	logger.Infof("[JUDGE] VLLM Judge container start triggered")

	pollingInterval := os.Getenv("LLM_CONTAINER_POLLING_INTERVAL")
	if pollingInterval == "" {
		pollingInterval = "30s"
	}

	duration, err := time.ParseDuration(pollingInterval)
	if err != nil {
		const defaultDuration = time.Duration(30)
		duration = defaultDuration * time.Second
	}

	time.Sleep(duration)

	count := 0
	for count <= 5 {
		if hasLLMServerStarted(podName) {
			logger.Infof("[JUDGE] VLLM as Judge container started successfully")

			return nil
		}
		time.Sleep(duration)
		count++
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
