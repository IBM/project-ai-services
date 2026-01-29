package bootstrap

import "fmt"

// HealthCheck performs a health check of the service (currently a placeholder).
func HealthCheck(baseURL string) error {
	fmt.Println("[BOOTSTRAP] Placeholder: health check for", baseURL)

	return nil
}
