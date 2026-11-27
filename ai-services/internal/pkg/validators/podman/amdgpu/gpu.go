package amdgpu

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/accelerator/amdgpu"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// AMDGPURule validates that AMD GPU devices are present and accessible.
type AMDGPURule struct{}

func NewAMDGPURule() *AMDGPURule {
	return &AMDGPURule{}
}

func (r *AMDGPURule) Name() string {
	return "amd-gpu"
}

func (r *AMDGPURule) Description() string {
	return "Validates that AMD GPU devices (/dev/kfd, /dev/dri) are present and accessible."
}

func (r *AMDGPURule) Verify(ctx context.Context) error {
	logger.Debugln("Validating AMD GPU availability...")

	if !amdgpu.IsApplicable() {
		return fmt.Errorf("AMD GPU not detected: /dev/kfd not found (is the amdgpu kernel module loaded?)")
	}

	// /dev/dri must also exist for ROCm rendering/compute contexts
	if _, err := os.Stat("/dev/dri"); err != nil {
		return fmt.Errorf("AMD GPU device directory /dev/dri not found: %w", err)
	}

	devices, err := amdgpu.ListDevices(ctx)
	if err != nil {
		return fmt.Errorf("failed to enumerate AMD GPU devices: %w", err)
	}

	if len(devices) == 0 {
		return fmt.Errorf("no AMD GPU devices found via lspci")
	}

	logger.Debugf("✓ AMD GPU validation passed: %d device(s) detected\n", len(devices))

	return nil
}

func (r *AMDGPURule) Message() string {
	return "AMD GPU devices are present and accessible"
}

func (r *AMDGPURule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *AMDGPURule) Hint() string {
	return "Ensure the amdgpu kernel module is loaded ('modprobe amdgpu') and the user has access to /dev/kfd and /dev/dri."
}

// ROCmVersionRule validates that the ROCm stack is installed on the host.
type ROCmVersionRule struct{}

func NewROCmVersionRule() *ROCmVersionRule {
	return &ROCmVersionRule{}
}

func (r *ROCmVersionRule) Name() string {
	return "rocm-version"
}

func (r *ROCmVersionRule) Description() string {
	return "Validates that the ROCm stack is installed and rocm-smi reports a version."
}

func (r *ROCmVersionRule) Verify(_ context.Context) error {
	logger.Debugln("Validating ROCm installation...")

	// Check ROCm version.
	out, err := amdgpu.ROCmSMICmd("--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("ROCm not detected: rocm-smi --version failed: %w", err)
	}

	version := strings.TrimSpace(string(out))
	if version == "" {
		return fmt.Errorf("ROCm not detected: rocm-smi --version returned no output")
	}

	logger.Debugf("✓ ROCm version: %s\n", version)

	// Probe that rocm-smi can enumerate GPU devices.
	out, err = amdgpu.ROCmSMICmd("--showid").CombinedOutput()
	if err != nil {
		return fmt.Errorf("rocm-smi probe failed: %w\noutput: %s", err, string(out))
	}

	// rocm-smi always prints header/footer lines; a GPU is present only when
	// at least one "GPU[N]" line appears in the output.
	gpuFound := false
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "GPU[") {
			gpuFound = true

			break
		}
	}

	if !gpuFound {
		return fmt.Errorf("rocm-smi --showid found no GPU devices")
	}

	logger.Debugf("✓ rocm-smi probe succeeded\n")

	return nil
}

func (r *ROCmVersionRule) Message() string {
	return "ROCm stack is installed and GPU devices are enumerable"
}

func (r *ROCmVersionRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *ROCmVersionRule) Hint() string {
	return "Install the ROCm stack from https://public.dhe.ibm.com/software/server/POWER/Linux/AMD/ROCm/RHEL/10/ppc64le/ before running bootstrap."
}
