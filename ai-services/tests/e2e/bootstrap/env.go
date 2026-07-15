package bootstrap

import (
	"os"
	"path/filepath"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	dirPerm  = 0o755 // rwxr-xr-x — directories
	execPerm = 0o755 // rwxr-xr-x — executable binaries
)

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

// GetPodManCreds returns the container registry credentials.
func GetPodManCreds() (registry string, username string, password string) {
	return os.Getenv("REGISTRY_URL"), os.Getenv("REGISTRY_USER_NAME"), os.Getenv("REGISTRY_PASSWORD")
}

// GetRHRegistryCreds returns Red Hat registry credentials.
func GetRHRegistryCreds() (registry string, username string, password string) {
	return os.Getenv("RH_REGISTRY_URL"), os.Getenv("RH_REGISTRY_USER_NAME"), os.Getenv("RH_REGISTRY_PASSWORD")
}

// GetLLMasJudgeModelDetails returns the LLM-as-Judge model path and name.
func GetLLMasJudgeModelDetails() (downloadPath string, modelName string) {
	return os.Getenv("LLM_JUDGE_MODEL_PATH"), os.Getenv("LLM_JUDGE_MODEL")
}

// GetLLMasJudgePodDetails returns the LLM-as-Judge container port and image.
func GetLLMasJudgePodDetails() (portNumber string, llmImage string) {
	return os.Getenv("LLM_JUDGE_PORT"), os.Getenv("LLM_JUDGE_IMAGE")
}

// GetCatalogCreds returns catalog credentials from environment variables.
func GetCatalogCreds() (serverURL string, username string, password string) {
	return os.Getenv("CATALOG_SERVER_URL"), catalogAdminUsername, GetCatalogAdminPassword()
}

// catalogAdminUsername is the fixed admin username across environments.
const catalogAdminUsername = "admin"

// GetCatalogAdminPassword returns CATALOG_PASSWORD; must always be set explicitly.
func GetCatalogAdminPassword() string {
	return os.Getenv("CATALOG_PASSWORD")
}

// GetCatalogInsecure returns true unless CATALOG_INSECURE=false is set.
// Defaults to true because e2e catalog uses self-signed / nip.io certificates.
func GetCatalogInsecure() bool {
	return os.Getenv("CATALOG_INSECURE") != "false"
}

// GetGoldenDatasetFile returns the name of the golden dataset file.
func GetGoldenDatasetFile() string {
	return os.Getenv("GOLDEN_DATASET_FILE")
}
