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

func startVLLMContainer(podName string, modelPath string) (err error) {
	logger.Infof("Starting the VLLM Container")

	llmJudgePort, llmImage := bootstrap.GetLLMasJudgePodDetails()

	command := "podman"
	// All arguments must be passed as a slice of strings
	args := []string{
		"run",
		"-d",
		"--name",
		podName,
		"-p",
		llmJudgePort + ":" + llmJudgePort,
		"-v",
		modelPath + ":/model:Z",
		"-e",
		"TORCHINDUCTOR_DISABLE=1",
		"-e",
		"TORCH_COMPILE=0",
		llmImage,
		"--model",
		"/model",
		"--tokenizer",
		"/model",
		"--dtype",
		"float32",
		"--enforce-eager",
		"--max-model-len",
		"4096",
		"--max-num-batched-tokens",
		"4096",
		"--served-model-name",
		Model,
	}

	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	err = cmd.Run()

	return err
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
	if output != "" {
		return true
	} else {
		return false
	}
}

// judgeModelAlreadyDownloaded returns true when ModelPath/Model exists as a
// non-empty directory — meaning a previous run already downloaded the weights.
func judgeModelAlreadyDownloaded() bool {
	modelDir := ModelPath + "/" + Model
	entries, err := os.ReadDir(modelDir)
	if err != nil {
		return false
	}
	return len(entries) > 0
}

// DownloadJudgeModel performs the registry login and model download steps of
// judge setup. It does NOT start the vLLM container — that is left to
// StartJudgeContainer, which should be called only after the main RAG LLM is
// confirmed ready (to avoid a GPU resource crunch).
//
// If the model weights are already present at ModelPath/Model the download is
// skipped entirely — cutting repeat-run BeforeAll time from ~2h to ~2min.
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

// StartJudgeContainer starts the vLLM judge container and waits for it to be
// ready. Call this only after DownloadJudgeModel has succeeded AND the main
// RAG LLM is confirmed ready via WaitForRAGBackendReady — starting the judge
// container while the main LLM is still loading causes a resource crunch that
// crashes OpenSearch and produces 0% accuracy.
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

// SetupLLMAsJudge is a convenience wrapper that runs DownloadJudgeModel then
// StartJudgeContainer in sequence. Kept for callers that do not need to
// overlap the download with other work.
func SetupLLMAsJudge(ctx context.Context, cfg *config.Config, runID string) error {
	if err := DownloadJudgeModel(ctx, cfg); err != nil {
		return err
	}
	return StartJudgeContainer(ctx, cfg, runID)
}

func CleanupLLMAsJudge(runID string) error {
	logger.Infof("Stopping the VLLM Container")

	command := "podman"
	stopArgs := []string{
		"stop",
		"vllm-judge-" + runID,
	}

	stopCmd := exec.Command(command, stopArgs...)
	stopCmd.Stdout = os.Stdout
	stopCmd.Stderr = os.Stderr
	stopCmd.Stdin = os.Stdin
	stopErr := stopCmd.Run()

	if stopErr != nil {
		logger.Errorf("error stopping the container: %v", stopErr)

		return fmt.Errorf("error stopping the container: %v", stopErr)
	}

	removeArgs := []string{
		"rm",
		"vllm-judge-" + runID,
	}

	removeCmd := exec.Command(command, removeArgs...)
	removeCmd.Stdout = os.Stdout
	removeCmd.Stderr = os.Stderr
	removeCmd.Stdin = os.Stdin
	removeErr := removeCmd.Run()

	if removeErr != nil {
		logger.Errorf("error removing the container: %v", removeErr)

		return fmt.Errorf("error stopping the container: %v", removeErr)
	}

	return nil
}
