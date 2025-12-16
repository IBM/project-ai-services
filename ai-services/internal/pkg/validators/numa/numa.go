package numa

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type NumaRule struct{}

func NewNumaRule() *NumaRule {
	return &NumaRule{}
}

func (r *NumaRule) Name() string {
	return "numa"
}

func (r *NumaRule) Verify() error {
	logger.Infoln("Validating NUMA node alignment on LPAR", 2)
	cmd := `lscpu | grep -i "NUMA node(s)"`
	out, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		return fmt.Errorf("failed to execute lscpu command: %w", err)
	}

	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return fmt.Errorf("failed to get NUMA node fields")
	}

	numaVal := fields[len(fields)-1]
	numaCount, err := strconv.Atoi(numaVal)
	if err != nil {
		return fmt.Errorf("error extracting numa count: %w", err)
	}

	if numaCount != 1 {
		return fmt.Errorf(`current NUMA node configuration (%d) is not aligned for maximum efficiency. For optimal performance, ensure that all CPUs are aligned to a single NUMA node`, numaCount)
	}

	return nil
}

func (r *NumaRule) Message() string {
	return "NUMA node alignment on LPAR: 1"
}

func (r *NumaRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelWarning
}

func (r *NumaRule) Hint() string {
	return fmt.Sprintf(`This tools requires numa node alignment set to 1 on LPAR. For optimal performance, ensure that all CPUs are aligned to a single NUMA node.
For detailed instructions and best practices on NUMA configuration, please refer to %shttps://www.ibm.com/docs/aiservices?topic=installation-chip-alignment-in-lpar%s`,
		"\033[34m",
		"\033[0m")
}
