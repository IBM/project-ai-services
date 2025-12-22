package bootstrap

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var testBinDir string // package-level var to store the temp bin directory

// SetTestBinDir sets the temporary directory for test binaries
func SetTestBinDir(dir string) {
	testBinDir = dir
}

// GetTestBinDir returns the temporary directory for test binaries
func GetTestBinDir() string {
	return testBinDir
}

// BuildOrVerifyCLIBinary ensures the ai-services binary is available.
// It checks (in order):
// 1. AI_SERVICES_BIN environment variable
// 2. Temp bin directory (from SetTestBinDir)
// 3. Builds the binary using make build (or go build fallback) and stores in temp directory
// 4. Verifies the binary works by running version command
// Returns the path to the binary or an error.
func BuildOrVerifyCLIBinary(ctx context.Context) (string, error) {
	fmt.Println("[BOOTSTRAP] Starting BuildOrVerifyCLIBinary...")

	// 1. Check environment variable
	if bin := strings.TrimSpace(os.Getenv("AI_SERVICES_BIN")); bin != "" {
		fmt.Printf("[BOOTSTRAP] Checking AI_SERVICES_BIN: %s\n", bin)
		_, err := checkBinaryVersion(bin)
		if err == nil {
			fmt.Printf("[BOOTSTRAP] ✓ Using AI_SERVICES_BIN: %s\n", bin)
			return bin, nil
		}
		return "", fmt.Errorf("AI_SERVICES_BIN=%s failed verification: %w", bin, err)
	}

	// 2. Check temp bin directory if set
	if testBinDir != "" {
		binPath := filepath.Join(testBinDir, "ai-services")
		fmt.Printf("[BOOTSTRAP] Checking for existing binary in temp dir: %s\n", binPath)
		_, err := checkBinaryVersion(binPath)
		if err == nil {
			fmt.Printf("[BOOTSTRAP] ✓ Found and verified binary at: %s\n", binPath)
			return binPath, nil
		}
		fmt.Printf("[BOOTSTRAP] Binary not found in temp dir, will build\n")
	}

	// 3. Build using make build (or go build fallback) into temp directory
	if testBinDir == "" {
		return "", fmt.Errorf("testBinDir not set; call SetTestBinDir before BuildOrVerifyCLIBinary")
	}

	fmt.Println("[BOOTSTRAP] Building ai-services...")
	binPath, err := buildBinary(ctx, testBinDir)
	if err != nil {
		fmt.Printf("[BOOTSTRAP] Build failed: %v\n", err)
		return "", err
	}

	// 4. Verify the built binary
	fmt.Printf("[BOOTSTRAP] Verifying built binary at: %s\n", binPath)
	_, err = checkBinaryVersion(binPath)
	if err != nil {
		return "", fmt.Errorf("built binary failed verification: %w", err)
	}

	fmt.Printf("[BOOTSTRAP] ✓ Successfully built and verified binary: %s\n", binPath)
	return binPath, nil
}

// buildBinary tries to build the binary using make build first, then falls back to go build
func buildBinary(ctx context.Context, tempBinDir string) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}
	fmt.Printf("[BOOTSTRAP] Current working directory: %s\n", cwd)

	// Determine ai-services module root
	moduleRoot := findAIServicesRoot(cwd)
	if moduleRoot == "" {
		return "", fmt.Errorf("could not find ai-services module root from %s", cwd)
	}
	fmt.Printf("[BOOTSTRAP] Using ai-services module root: %s\n", moduleRoot)

	// Check if Makefile exists in ai-services directory
	makefilePath := filepath.Join(moduleRoot, "Makefile")
	if _, err := os.Stat(makefilePath); err == nil {
		fmt.Printf("[BOOTSTRAP] Found Makefile at: %s\n", makefilePath)
		fmt.Println("[BOOTSTRAP] Attempting to build using 'make build'...")

		binPath, err := buildUsingMake(ctx, moduleRoot, tempBinDir)
		if err == nil {
			return binPath, nil
		}
		fmt.Printf("[BOOTSTRAP] 'make build' failed: %v\n", err)
		fmt.Println("[BOOTSTRAP] Falling back to 'go build'...")
	} else {
		fmt.Printf("[BOOTSTRAP] Makefile not found at %s, using 'go build' directly\n", makefilePath)
	}

	// Fallback to go build
	return buildUsingGo(ctx, moduleRoot, tempBinDir)
}

// buildUsingMake runs `make build` in the ai-services directory and copies the binary to the temp directory.
func buildUsingMake(ctx context.Context, moduleRoot string, tempBinDir string) (string, error) {
	// Run make build
	cmd := exec.CommandContext(ctx, "make", "build")
	cmd.Dir = moduleRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("[BOOTSTRAP] Running 'make build' in %s\n", moduleRoot)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("make build failed: %w", err)
	}

	fmt.Println("[BOOTSTRAP] 'make build' completed successfully")

	// Check for binary in expected location after make
	srcBinPath := filepath.Join(moduleRoot, "bin", "ai-services")
	if _, err := os.Stat(srcBinPath); err != nil {
		return "", fmt.Errorf("binary not found at %s after make build: %w", srcBinPath, err)
	}
	fmt.Printf("[BOOTSTRAP] Found built binary at: %s\n", srcBinPath)

	// Copy to temp directory
	return copyBinaryToTemp(srcBinPath, tempBinDir)
}

// buildUsingGo runs `go build` to compile the binary directly
func buildUsingGo(ctx context.Context, moduleRoot string, tempBinDir string) (string, error) {
	// Create temp bin directory if it doesn't exist
	if err := os.MkdirAll(tempBinDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp bin directory: %w", err)
	}

	destBinPath := filepath.Join(tempBinDir, "ai-services")

	// Run go build
	cmd := exec.CommandContext(ctx, "go", "build", "-o", destBinPath, "./cmd/ai-services")
	cmd.Dir = moduleRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("[BOOTSTRAP] Running 'go build -o %s ./cmd/ai-services' in %s\n", destBinPath, moduleRoot)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("go build failed: %w", err)
	}

	fmt.Println("[BOOTSTRAP] 'go build' completed successfully")

	// Verify binary was created
	if _, err := os.Stat(destBinPath); err != nil {
		return "", fmt.Errorf("binary not found at %s after go build: %w", destBinPath, err)
	}

	fmt.Printf("[BOOTSTRAP] Binary built and placed at: %s\n", destBinPath)
	return destBinPath, nil
}

// copyBinaryToTemp copies the built binary from source to temp directory
func copyBinaryToTemp(srcBinPath string, tempBinDir string) (string, error) {
	// Create temp bin directory if it doesn't exist
	if err := os.MkdirAll(tempBinDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create temp bin directory: %w", err)
	}

	// Copy binary to temp directory
	destBinPath := filepath.Join(tempBinDir, "ai-services")
	fmt.Printf("[BOOTSTRAP] Copying binary from %s to %s\n", srcBinPath, destBinPath)

	srcFile, err := os.Open(srcBinPath)
	if err != nil {
		return "", fmt.Errorf("failed to open source binary: %w", err)
	}
	defer srcFile.Close()

	destFile, err := os.Create(destBinPath)
	if err != nil {
		return "", fmt.Errorf("failed to create destination binary: %w", err)
	}
	defer destFile.Close()

	// Copy file content
	if _, err := io.Copy(destFile, srcFile); err != nil {
		return "", fmt.Errorf("failed to copy binary content: %w", err)
	}

	// Make it executable
	if err := os.Chmod(destBinPath, 0755); err != nil {
		return "", fmt.Errorf("failed to make binary executable: %w", err)
	}

	fmt.Printf("[BOOTSTRAP] Binary copied and made executable: %s\n", destBinPath)
	return destBinPath, nil
}

// checkBinaryVersion checks if the binary exists and tries different version command formats.
// It returns the version output and any error encountered.
// Tries: `ai-services version`, `ai-services --version`, `ai-services -v`
func checkBinaryVersion(binPath string) (string, error) {
	// Check if file exists
	info, err := os.Stat(binPath)
	if err != nil {
		return "", fmt.Errorf("binary not found at %s: %w", binPath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory, not a binary: %s", binPath)
	}

	// List of version command formats to try
	versionCmds := []string{"version", "--version", "-v"}

	var lastErr error
	for _, versionCmd := range versionCmds {
		cmd := exec.Command(binPath, versionCmd)
		output, err := cmd.CombinedOutput()

		if err == nil {
			versionStr := strings.TrimSpace(string(output))
			if versionStr != "" {
				return versionStr, nil
			}
		}
		lastErr = err
	}

	// If all commands failed, return the last error
	return "", fmt.Errorf("all version commands failed. Last error: %w", lastErr)
}

// GetBinaryVersion returns the version of the ai-services binary by trying all version command formats
// Silent function - returns version without printing
func GetBinaryVersion(binPath string) (string, error) {
	return checkBinaryVersion(binPath)
}

// findAIServicesRoot locates the ai-services module root by looking for go.mod.
func findAIServicesRoot(startPath string) string {
	for d := startPath; d != "/" && d != ""; d = filepath.Dir(d) {
		gomod := filepath.Join(d, "go.mod")
		if info, err := os.Stat(gomod); err == nil && !info.IsDir() {
			content, err := os.ReadFile(gomod)
			if err == nil {
				modContent := string(content)
				if strings.Contains(modContent, "ai-services") {
					return d
				}
			}
		}
	}
	return ""
}
