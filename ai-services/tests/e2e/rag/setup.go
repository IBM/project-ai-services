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

func SetupLLMAsJudge(ctx context.Context, cfg *config.Config, runID string) (err error) {
	logger.Infof("Setting up LLM as Judge")

	// podman login using RH registry creds
	url, uname, psswd := bootstrap.GetRHRegistryCreds()
	loginErr := bootstrap.PodmanRegistryLogin(url, uname, psswd)

	if loginErr != nil {
		logger.Errorf("error performing registry login %v", loginErr)

		return fmt.Errorf("error performing registry login %v", loginErr)
	}
	logger.Infof("RH Registry login completed")

	// download the model using ai services helper
	modelErr := helpers.DownloadModel(Model, ModelPath)

	if modelErr != nil {
		logger.Errorf("error downloading LLM as Judge model %v", modelErr)

		return fmt.Errorf("error downloading LLM as Judge model %v", modelErr)
	}
	logger.Infof("VLLM Judge model download completed")

	// start podman container
	podName := "vllm-judge-" + runID
	runErr := startVLLMContainer(podName, ModelPath+"/"+Model)
	if runErr != nil {
		logger.Errorf("error running LLM as Judge container %v", runErr)

		return fmt.Errorf("error running LLM as Judge container %v", runErr)
	}
	logger.Infof("VLLM Judge container start triggered")

	//wait for polling interval and monitor the pod logs to check if server has started
	pollingInterval := os.Getenv("LLM_CONTAINER_POLLING_INTERVAL")
	if pollingInterval == "" {
		pollingInterval = "30s" //default polling interval to 30 seconds
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
			logger.Infof("VLLM as Judge container started successfully")

			return nil
		} else {
			time.Sleep(duration)
			count++
		}
	}

	logger.Errorf("polling attempts exhausted. VLLM Judge server was not started")

	return fmt.Errorf("polling attempts exhausted. VLLM Judge server was not started")
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
