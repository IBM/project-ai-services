package utils

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	"github.com/containers/podman/v5/pkg/bindings/system"
	"github.com/jaypipes/ghw"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

const (
	// FilePermissions defines the default file permissions (rw-r--r--).
	FilePermissions = 0644
)

// ExecuteCommand executes a shell command and returns exit code, stdout, and stderr.
func ExecuteCommand(command string, args ...string) (int, string, string, error) {
	cmd := exec.Command(command, args...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return -1, "", "", err
		}
	}

	return exitCode, stdout.String(), stderr.String(), nil
}

// IsReadWriteToOwnerGroupUsers checks if a file has 0660 permissions.
func IsReadWriteToOwnerGroupUsers(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	mode := info.Mode().Perm()
	// Check for 0660 permissions (rw-rw----)
	return mode&0660 == 0660 && mode&0007 == 0
}

// FileExists checks if a file exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// ReadFileLines reads a file and returns its lines.
func ReadFileLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return lines, nil
}

// GroupExists checks if a group exists.
func GroupExists(groupName string) bool {
	_, err := user.LookupGroup(groupName)

	return err == nil
}

// GetGroupID returns the GID for a group name.
func GetGroupID(groupName string) (int, error) {
	group, err := user.LookupGroup(groupName)
	if err != nil {
		return -1, err
	}

	gid, err := strconv.Atoi(group.Gid)
	if err != nil {
		return -1, err
	}

	return gid, nil
}

// GetFileGroupID returns the group ID of a file.
func GetFileGroupID(path string) (int, error) {
	info, err := os.Stat(path)
	if err != nil {
		return -1, err
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return -1, fmt.Errorf("failed to get file stat")
	}

	return int(stat.Gid), nil
}

// IsModuleLoaded checks if a kernel module is loaded.
func IsModuleLoaded(moduleName string) bool {
	exitCode, stdout, _, err := ExecuteCommand("lsmod")
	if err != nil || exitCode != 0 {
		return false
	}

	lines := strings.Split(stdout, "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == moduleName {
			return true
		}
	}

	return false
}

// GetPCIInfo returns PCI device information using ghw.
func GetPCIInfo() (*ghw.PCIInfo, error) {
	// Capture stderr to filter out specific ghw warnings
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Call ghw.PCI()
	pciInfo, err := ghw.PCI()

	// Restore stderr
	_ = w.Close()
	os.Stderr = oldStderr

	// Read captured stderr and filter out the specific warning.
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	stderrOutput := buf.String()

	// Only print stderr lines that are NOT the CPU topology warning.
	if stderrOutput != "" {
		lines := strings.Split(stderrOutput, "\n")
		for _, line := range lines {
			if line != "" && !strings.Contains(line, "WARNING: failed to read int from file") &&
				!strings.Contains(line, "topology/core_id: no such file or directory") {
				_, _ = fmt.Fprintln(oldStderr, line)
			}
		}
	}

	return pciInfo, err
}

// AppendToFile appends content to a file.
func AppendToFile(path, content string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, FilePermissions)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	_, err = f.WriteString(content)

	return err
}

// WriteToFile writes content to a file, overwriting if it exists.
func WriteToFile(path, content string) error {
	return os.WriteFile(path, []byte(content), FilePermissions)
}

// executeCommandOrFail executes a command and returns an error if it fails.
func executeCommandOrFail(command string, args ...string) error {
	exitCode, _, stderr, err := ExecuteCommand(command, args...)
	if err != nil {
		return err
	}

	if exitCode != 0 {
		return fmt.Errorf("command failed: %s", stderr)
	}

	return nil
}

// CreateGroup creates a system group.
func CreateGroup(groupName string) error {
	if err := executeCommandOrFail("groupadd", groupName); err != nil {
		return fmt.Errorf("failed to create group: %w", err)
	}

	return nil
}

// LoadKernelModule loads a kernel module.
func LoadKernelModule(moduleName string) error {
	if err := executeCommandOrFail("modprobe", moduleName); err != nil {
		return fmt.Errorf("failed to load module: %w", err)
	}

	return nil
}

// ReloadUdevRules reloads udev rules.
func ReloadUdevRules() error {
	if err := executeCommandOrFail("udevadm", "control", "--reload-rules"); err != nil {
		return fmt.Errorf("failed to reload udev rules: %w", err)
	}

	if err := executeCommandOrFail("udevadm", "trigger", "--subsystem-match=vfio"); err != nil {
		return fmt.Errorf("failed to trigger udev: %w", err)
	}

	if err := executeCommandOrFail("udevadm", "settle"); err != nil {
		return fmt.Errorf("failed to settle udev: %w", err)
	}

	return nil
}

// Podman checks if podman is installed and available in PATH.
func Podman() (string, error) {
	path, err := exec.LookPath("podman")
	if err != nil {
		return "", fmt.Errorf("podman is not installed or not found in PATH, error: %v", err)
	}

	return path, nil
}

// PodmanHealthCheck verifies podman is working.
func PodmanHealthCheck() error {
	client, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to create podman client: %w", err)
	}

	version, err := system.Version(client.Context, nil)
	if err != nil {
		return fmt.Errorf("podman health check failed (cannot get version): %w", err)
	}

	if version.Server == nil || version.Server.Version == "" {
		return fmt.Errorf("podman health check failed (invalid version info)")
	}

	return nil
}

// Made with Bob
