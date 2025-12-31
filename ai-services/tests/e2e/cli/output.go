package cli

import (
	"fmt"
	"strings"
)

// ValidateBootstrapOutput ensures required bootstrap output is present
func ValidateBootstrapOutput(output string) error {
	required := []string{
		"LPAR configured successfully",  // configure step
		"All validations passed",        // validate step
		"LPAR boostrapped successfully", // full bootstrap confirmation
	}

	for _, r := range required {
		if !strings.Contains(output, r) {
			return fmt.Errorf("bootstrap validation failed: missing '%s'", r)
		}
	}

	return nil
}
