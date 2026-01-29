package common

import (
	"os/exec"
)

// RunCommand executes shell commands and returns output.
func RunCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()

	return string(out), err
}
