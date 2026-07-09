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

// GetCatalogCreds returns the catalog API server credentials from environment variables.
//
//	CATALOG_SERVER_URL  – base URL of the catalog API server (e.g. http://localhost:8080)
//	CATALOG_USERNAME    – username to authenticate with (constant: "admin")
//	CATALOG_PASSWORD    – password to authenticate with (default: "1234")
func GetCatalogCreds() (serverURL string, username string, password string) {
	return os.Getenv("CATALOG_SERVER_URL"), catalogAdminUsername, GetCatalogAdminPassword()
}

// catalogAdminUsername is the fixed admin username — never changes across environments.
const catalogAdminUsername = "admin"

// GetCatalogAdminPassword returns the catalog admin password.
// Defaults to "1234" (the known e2e default) so CATALOG_PASSWORD does not need
// to be exported manually before running tests.
// Override by setting CATALOG_PASSWORD in the environment.
func GetCatalogAdminPassword() string {
	if v := os.Getenv("CATALOG_PASSWORD"); v != "" {
		return v
	}

	return "1234"
}

// GetCatalogInsecure returns true when TLS certificate verification should be skipped for the catalog server.
// This is the default for e2e environments because the catalog uses nip.io / self-signed certificates.
// Set CATALOG_INSECURE=false to force strict TLS verification.
func GetCatalogInsecure() bool {
	v := os.Getenv("CATALOG_INSECURE")
	// Default is true (skip verification) — e2e catalog always uses self-signed certs.
	// Only disable when explicitly set to "false".
	return v != "false"
}

// GetGoldenDatasetFile returns the name of the golden dataset file.
func GetGoldenDatasetFile() string {
	return os.Getenv("GOLDEN_DATASET_FILE")
}
