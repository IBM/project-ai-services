package utils

import (
	"fmt"
	"os/exec"
	"strings"
)

// PodmanRun executes `podman <args>` via the CLI and returns combined stdout+stderr.
func PodmanRun(args ...string) ([]byte, error) {
	out, err := exec.Command("podman", args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("podman %s: %w (output: %s)",
			strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}

	return out, nil
}

// PodmanContainerName extracts the container name from a `podman ps --format json` entry.
// The "Names" field is a JSON array of strings.
func PodmanContainerName(c map[string]any) string {
	switch v := c["Names"].(type) {
	case []any:
		if len(v) > 0 {
			return fmt.Sprintf("%v", v[0])
		}
	case string:
		return v
	}

	return ""
}
