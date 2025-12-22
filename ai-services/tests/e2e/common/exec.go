package common

import (
	"log"
	"os/exec"
)

// RunCommand executes shell commands and returns output
func RunCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	log.Println(string(out))
	return string(out), err
}
