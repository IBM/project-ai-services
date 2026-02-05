package bootstrap

import (
	"os"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// dirPerm defines the default permission for created directories.
const dirPerm = 0o755 // standard read/write/execute for owner, read/execute for group and others

// PrepareRuntime creates isolated temp directories for tests.
func PrepareRuntime(runID string) string {
	tempDir := filepath.Join("/tmp/ais-e2e", runID)
	if err := os.MkdirAll(tempDir, dirPerm); err != nil {
		logger.Errorf("[BOOTSTRAP] Failed to create temp directory: %v", err)

		return ""
	}

	if err := os.Setenv("AI_SERVICES_HOME", tempDir); err != nil {
		logger.Errorf("[BOOTSTRAP] Failed to set AI_SERVICES_HOME: %v", err)
	}

	logger.Infof("[BOOTSTRAP] Temp runtime environment created at: %s", tempDir)
	
	return tempDir
}

// GetRuntimeDir returns the AI_SERVICES_HOME directory.
func GetRuntimeDir() string {
	return os.Getenv("AI_SERVICES_HOME")
}

// GetPodManCreds returns the registry details.
func GetPodManCreds() (registry string, username string, password string) {
	return os.Getenv("REGISTRY_URL"), os.Getenv("REGISTRY_USER_NAME"), os.Getenv("REGISTRY_PASSWORD")
}

// GetRHRegistryCreds returns the RedHat registry details.
func GetRHRegistryCreds() (registry string, username string, password string) {
	return os.Getenv("RH_REGISTRY_URL"), os.Getenv("RH_REGISTRY_USER_NAME"), os.Getenv("RH_REGISTRY_PASSWORD")
}

// GetLLMasJudgeModelDetails returns the registry details.
func GetLLMasJudgeModelDetails() (downloadPath string, modelName string) {
	return os.Getenv("LLM_JUDGE_MODEL_PATH"), os.Getenv("LLM_JUDGE_MODEL")
}

// GetLLMasJudgePodDetails returns the registry details.
func GetLLMasJudgePodDetails() (portNumber string, llmImage string) {
	return os.Getenv("LLM_JUDGE_PORT"), os.Getenv("LLM_JUDGE_IMAGE")
}
