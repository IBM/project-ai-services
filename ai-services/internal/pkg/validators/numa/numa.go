package numa

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

type NumaRule struct{}

func NewNumaRule() *NumaRule {
	return &NumaRule{}
}

func (r *NumaRule) Name() string {
	return "numa"
}

func (r *NumaRule) Verify() error {
	logger.Infoln("Validating Numa Node alignment on LPAR", 2)
	cmd := `cat /proc/ppc64/lparcfg  | grep affinity`
	out, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		return fmt.Errorf("failed to check affinity score on LPAR: %w", err)
	}

	fields := strings.Split(string(out), "=")
	if len(fields) != 2 {
		return fmt.Errorf("failed to get affinity score")
	}

	affinityScoreStr := fields[1]
	affinityScore, err := strconv.Atoi(strings.Trim(affinityScoreStr, "\n"))
	if err != nil {
		return fmt.Errorf("error extracting affinity score: %w", err)
	}

	if affinityScore < vars.LparAffinityThreshold {
		return fmt.Errorf("the current LPAR affinity score (%d) is not matching the threshold %d", affinityScore, vars.LparAffinityThreshold)
	}

	return nil
}

func (r *NumaRule) Message() string {
	return fmt.Sprintf("LPAR affinity score is above the threshold: %d", vars.LparAffinityThreshold)
}

func (r *NumaRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelWarning
}

func (r *NumaRule) Hint() string {
	return fmt.Sprintf("LPAR affinity score needs to be above the threshold: %d", vars.LparAffinityThreshold)
}
