package numa

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const defaultSysfsRoot = "/sys/devices/system/node"

// nodeDir matches kernel NUMA node directories: "node" followed by one or more digits.
var nodeDirRe = regexp.MustCompile(`^node\d+$`)

// NumaRule validates that all LPAR CPUs are confined to exactly one NUMA node.
// It reads the kernel's own sysfs data instead of parsing lscpu output, so
// empty firmware-topology-placeholder nodes (which carry no CPUs or memory)
// are not counted as active nodes.
type NumaRule struct {
	// sysfsRoot is the path to the node directory; overridden in tests.
	sysfsRoot string
}

func NewNumaRule() *NumaRule {
	return &NumaRule{sysfsRoot: defaultSysfsRoot}
}

func (r *NumaRule) Name() string {
	return "numa"
}

func (r *NumaRule) Description() string {
	return "Validates that the NUMA node alignment on LPAR is set to 1 for optimal performance."
}

// Verify counts how many NUMA nodes are "active" — i.e. have at least one CPU
// or any non-zero MemTotal — and requires that count to be exactly 1.
func (r *NumaRule) Verify(_ context.Context) error {
	logger.Debugln("Validating NUMA node alignment on LPAR")

	root := r.sysfsRoot
	if root == "" {
		root = defaultSysfsRoot
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return fmt.Errorf("failed to read NUMA node directory %s: %w", root, err)
	}

	var activeNodes []string
	for _, e := range entries {
		if !e.IsDir() || !nodeDirRe.MatchString(e.Name()) {
			continue
		}
		nodeDir := filepath.Join(root, e.Name())
		active, err := isActiveNode(nodeDir)
		if err != nil {
			return fmt.Errorf("failed to check NUMA node %s: %w", e.Name(), err)
		}
		if active {
			activeNodes = append(activeNodes, e.Name())
		}
	}

	if len(activeNodes) == 0 {
		return fmt.Errorf("no active NUMA nodes found under %s", root)
	}

	if len(activeNodes) != 1 {
		return fmt.Errorf(
			"current NUMA node configuration (%d) is not aligned for maximum efficiency. "+
				"For optimal performance, ensure that all CPUs are aligned to a single NUMA node "+
				"(active nodes: %s)",
			len(activeNodes), strings.Join(activeNodes, ", "),
		)
	}

	return nil
}

// isActiveNode returns true if the node directory contains at least one CPU
// (non-empty cpulist) or any non-zero MemTotal in its meminfo.
// It returns an error if either file cannot be read or meminfo cannot be parsed.
func isActiveNode(dir string) (bool, error) {
	cpulist, err := os.ReadFile(filepath.Join(dir, "cpulist"))
	if err != nil {
		return false, fmt.Errorf("reading cpulist: %w", err)
	}
	if len(strings.TrimSpace(string(cpulist))) > 0 {
		return true, nil
	}

	memBytes, err := os.ReadFile(filepath.Join(dir, "meminfo"))
	if err != nil {
		return false, fmt.Errorf("reading meminfo: %w", err)
	}
	kb, err := parseMemTotalKB(string(memBytes))
	if err != nil {
		return false, fmt.Errorf("parsing meminfo: %w", err)
	}

	return kb > 0, nil
}

// parseMemTotalKB extracts the MemTotal value in kB from a sysfs meminfo string.
// The kernel format is:
//
//	Node N MemTotal:     <value> kB
//
// It finds the "MemTotal:" token and parses the following field as a uint64.
func parseMemTotalKB(meminfo string) (uint64, error) {
	fields := strings.Fields(meminfo)
	for i, f := range fields {
		if f == "MemTotal:" && i+1 < len(fields) {
			return strconv.ParseUint(fields[i+1], 10, 64)
		}
	}

	return 0, fmt.Errorf("MemTotal not found in meminfo")
}

func (r *NumaRule) Message() string {
	return "NUMA node alignment on LPAR: 1"
}

func (r *NumaRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelWarning
}

func (r *NumaRule) Hint() string {
	return fmt.Sprintf(`This tool requires numa node alignment set to 1 on LPAR. For optimal performance, ensure that all CPUs are aligned to a single NUMA node.
For detailed instructions and best practices on NUMA configuration, please refer to %shttps://www.ibm.com/docs/aiservices?topic=installation-chip-alignment-in-lpar%s`,
		"\033[34m",
		"\033[0m")
}
