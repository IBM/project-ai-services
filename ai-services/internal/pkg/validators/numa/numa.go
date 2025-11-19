package numa

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

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
	logger.Infoln("Validating Numa Node config...", 2)
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
		return fmt.Errorf("numa node on LPAR is %d, please set NUMA node to 1", numaCount)
	}

	return nil
}

func (r *NumaRule) Message() string {
	return "NUMA node alignment on LPAR: 1"
}
