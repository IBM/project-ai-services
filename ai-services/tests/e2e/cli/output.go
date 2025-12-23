package cli

import (
	"fmt"
	"strings"
)

// ValidateOutput validates CLI output and filesystem state

func ValidateBootstrapOutput(output string) error {
	required := []string{
		"LPAR configured successfully",
		"All validations passed",
		"LPAR bootstrapped successfully",
	}

	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("bootstrap validation failed: missing '%s'", r)
		}
	}

	return nil
}
