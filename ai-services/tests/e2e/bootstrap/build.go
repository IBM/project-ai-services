package bootstrap

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const execPerm = 0o755

var testBinDir string

// SetTestBinDir sets the temporary directory for test binaries.
func SetTestBinDir(dir string) {
	testBinDir = dir
}

// GetTestBinDir returns the temporary directory for test binaries.
func GetTestBinDir() string {
	return testBinDir
}

// BuildOrVerifyCLIBinary ensures the ai-services binary is available.
func BuildOrVerifyCLIBinary(ctx context.Context) (string, error) {
	logger.Infof("[BOOTSTRAP] Starting BuildOrVerifyCLIBinary...")

	if bin, ok, err := fromEnvBinary(); ok {
		return bin, err
	}

	if bin, ok := fromTempDirBinary(); ok {
		return bin, nil
	}

	return buildAndVerifyBinary(ctx)
}

// fromEnvBinary checks AI_SERVICES_BIN env variable.
func fromEnvBinary() (string, bool, error) {
	bin := strings.TrimSpace(os.Getenv("AI_SERVICES_BIN"))
	if bin == "" {
		return "", false, nil
	}

	logger.Infof("[BOOTSTRAP] AI_SERVICES_BIN is set: %s (validating)", bin)

	if _, err := CheckBinaryVersion(bin); err != nil {
		return "", true, fmt.Errorf(
			"AI_SERVICES_BIN=%s failed verification: %w",
			bin,
			err,
		)
	}

	logger.Infof("[BOOTSTRAP] Using AI_SERVICES_BIN: %s", bin)

	return bin, true, nil
}

// fromTempDirBinary checks existing binary in temp dir.
func fromTempDirBinary() (string, bool) {
	if testBinDir == "" {
		return "", false
	}

	binPath := filepath.Join(testBinDir, "ai-services")
	logger.Infof("[BOOTSTRAP] Checking for existing binary in temp dir: %s", binPath)

	if _, err := CheckBinaryVersion(binPath); err == nil {
		logger.Infof("[BOOTSTRAP] Found and verified binary at: %s", binPath)

		return binPath, true
	}

	logger.Infof("[BOOTSTRAP] Binary not found or invalid in temp dir")

	return "", false
}

// buildAndVerifyBinary builds and validates the binary.
func buildAndVerifyBinary(ctx context.Context) (string, error) {
	if testBinDir == "" {
		return "", fmt.Errorf(
			"testBinDir not set; call SetTestBinDir before BuildOrVerifyCLIBinary",
		)
	}

	logger.Infof("[BOOTSTRAP] Building ai-services...")

	binPath, err := buildBinary(ctx, testBinDir)
	if err != nil {
		logger.Errorf("[BOOTSTRAP] Build failed: %v", err)

		return "", err
	}

	logger.Infof("[BOOTSTRAP] Verifying built binary at: %s", binPath)

	if _, err := CheckBinaryVersion(binPath); err != nil {
		logger.Errorf("[BOOTSTRAP] Verification failed, removing invalid binary: %s", binPath)
		_ = os.Remove(binPath)

		return "", fmt.Errorf(
			"built binary failed verification: %w",
			err,
		)
	}

	logger.Infof("[BOOTSTRAP] Successfully built and verified binary: %s", binPath)

	return binPath, nil
}

// buildBinary tries make build first, then go build.
func buildBinary(ctx context.Context, tempBinDir string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	moduleRoot := findAIServicesRoot(cwd)
	if moduleRoot == "" {
		return "", fmt.Errorf("could not find ai-services module root from %s", cwd)
	}

	makefilePath := filepath.Join(moduleRoot, "Makefile")
	if _, err := os.Stat(makefilePath); err == nil {
		binPath, err := buildUsingMake(ctx, moduleRoot, tempBinDir)
		if err == nil {
			return binPath, nil
		}
	}

	return buildUsingGo(ctx, moduleRoot, tempBinDir)
}

// buildUsingMake runs `make build`.
func buildUsingMake(
	ctx context.Context,
	moduleRoot string,
	tempBinDir string,
) (string, error) {
	cmd := exec.CommandContext(ctx, "make", "build")
	cmd.Dir = moduleRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("make build failed: %w", err)
	}

	srcBinPath := filepath.Join(moduleRoot, "bin", "ai-services")
	if _, err := os.Stat(srcBinPath); err != nil {
		return "", fmt.Errorf("binary not found after make build: %w", err)
	}

	return copyBinaryToTemp(srcBinPath, tempBinDir)
}

// buildUsingGo runs `go build`.
func buildUsingGo(
	ctx context.Context,
	moduleRoot string,
	tempBinDir string,
) (string, error) {
	if err := os.MkdirAll(tempBinDir, execPerm); err != nil {
		return "", fmt.Errorf("failed to create temp bin directory: %w", err)
	}

	destBinPath := filepath.Join(tempBinDir, "ai-services")
	cmd := exec.CommandContext(
		ctx,
		"go",
		"build",
		"-o",
		destBinPath,
		"./cmd/ai-services",
	)
	cmd.Dir = moduleRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build failed: %w", err)
	}

	return destBinPath, nil
}

// copyBinaryToTemp copies binary to temp directory.
func copyBinaryToTemp(srcBinPath, tempBinDir string) (string, error) {
	if err := os.MkdirAll(tempBinDir, execPerm); err != nil {
		return "", fmt.Errorf("failed to create temp bin directory: %w", err)
	}

	destBinPath := filepath.Join(tempBinDir, "ai-services")

	srcFile, err := os.Open(srcBinPath)
	if err != nil {
		return "", fmt.Errorf("failed to open source binary: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	destFile, err := os.Create(destBinPath)
	if err != nil {
		return "", fmt.Errorf("failed to create destination binary: %w", err)
	}
	defer func() { _ = destFile.Close() }()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return "", fmt.Errorf("failed to copy binary: %w", err)
	}

	if err := os.Chmod(destBinPath, execPerm); err != nil {
		return "", fmt.Errorf("failed to execute binary: %w", err)
	}

	return destBinPath, nil
}

// CheckBinaryVersion verifies binary exists and runs version command.
func CheckBinaryVersion(binPath string) (string, error) {
	info, err := os.Stat(binPath)
	if err != nil {
		return "", fmt.Errorf("binary not found: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory: %s", binPath)
	}

	for _, arg := range []string{"version", "--version", "-v"} {
		cmd := exec.Command(binPath, arg)
		out, err := cmd.CombinedOutput()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return strings.TrimSpace(string(out)), nil
		}
	}

	return "", fmt.Errorf("binary version check failed")
}

// findAIServicesRoot locates module root via go.mod.
func findAIServicesRoot(startPath string) string {
	for d := startPath; d != "/" && d != ""; d = filepath.Dir(d) {
		gomod := filepath.Join(d, "go.mod")
		if content, err := os.ReadFile(gomod); err == nil &&
			strings.Contains(string(content), "ai-services") {
			return d
		}
	}

	return ""
}
