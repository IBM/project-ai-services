package spyre

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type SpyreRule struct{}

func NewSpyreRule() *SpyreRule {
	return &SpyreRule{}
}

func (r *SpyreRule) Name() string {
	return "spyre"
}

func (r *SpyreRule) Description() string {
	return "Validates that the IBM Spyre Accelerator is attached to the LPAR."
}

func (r *SpyreRule) Verify() error {
	logger.Infoln("Validating Spyre attachment...", logger.VerbosityLevelDebug)
	cmd := `lspci -k -d 1014:06a7 | wc -l`
	out, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		return fmt.Errorf("❌ failed to execute lspci command %w", err)
	}
	cardsCount, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return fmt.Errorf("failed to parse spyre cards count: %w", err)
	}
	if cardsCount == 0 {
		return fmt.Errorf("IBM Spyre Accelerator is not attached to the LPAR")
	}

	return nil
}

func (r *SpyreRule) Message() string {
	return "IBM Spyre Accelerator is attached to the LPAR"
}

func (r *SpyreRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *SpyreRule) Hint() string {
	return "IBM Spyre Accelerator hardware is required but not detected."
}
