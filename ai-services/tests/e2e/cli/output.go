package cli

import (
	"fmt"
	"strings"
)

func ValidateBootstrapConfigureOutput(output string) error {
	required := []string{
		"LPAR configured successfully",
		"Bootstrap configuration completed successfully",
	}
	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("bootstrap configure validation failed: missing '%s'", r)
		}
	}
	return nil
}
func ValidateBootstrapValidateOutput(output string) error {
	required := []string{
		"All validations passed",
	}
	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("bootstrap validate validation failed: missing '%s'", r)
		}
	}
	return nil
}
func ValidateBootstrapFullOutput(output string) error {
	required := []string{
		"LPAR configured successfully",
		"All validations passed",
	}
	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("full bootstrap validation failed: missing '%s'", r)
		}
	}
	return nil
}

func ValidateCreateAppOutput(output, appName string) error {
	required := []string{
		"All validations passed",
		fmt.Sprintf("Creating application '%s'", appName),
		fmt.Sprintf("Application '%s' deployed successfully", appName),
		"Next Steps:",
	}

	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("create-app validation failed: missing '%s'", r)
		}
	}

	if !strings.Contains(output, "Chatbot UI is available to use at http://") {
		return fmt.Errorf("create-app validation failed: RAG chatbot URL missing")
	}

	return nil
}
