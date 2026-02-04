package bootstrap

import "github.com/project-ai-services/ai-services/internal/pkg/logger"

// HealthCheck performs a health check of the service (currently a placeholder).
func HealthCheck(baseURL string) error {
	logger.Infoln("[BOOTSTRAP] Placeholder: health check for" + baseURL)

	return nil
}
