package validators

import (
	"context"
	"sync"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	kubeconfig "github.com/project-ai-services/ai-services/internal/pkg/validators/openshift/kubeconfig"
	nodelabels "github.com/project-ai-services/ai-services/internal/pkg/validators/openshift/nodelabels"
	operators "github.com/project-ai-services/ai-services/internal/pkg/validators/openshift/operators"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/openshift/rhods"
	spyrepolicy "github.com/project-ai-services/ai-services/internal/pkg/validators/openshift/spyreclusterpolicy"
	storageclass "github.com/project-ai-services/ai-services/internal/pkg/validators/openshift/storageclass"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/amdgpu"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/numa"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/platform"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/power"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/rhn"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/slicelimits"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/spyre"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/ulimits"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/usergroup"
)

// Initialize the default registry with built-in rules.
func init() {
	// Podman checks (Spyre)
	PodmanRegistry.Register(numa.NewNumaRule())
	PodmanRegistry.Register(platform.NewPlatformRule())
	PodmanRegistry.Register(power.NewPowerRule())
	PodmanRegistry.Register(rhn.NewRHNRule())
	PodmanRegistry.Register(spyre.NewSpyreRule())
	PodmanRegistry.Register(usergroup.NewUsergroupRule())
	PodmanRegistry.Register(ulimits.NewUlimitsRule())
	PodmanRegistry.Register(slicelimits.NewSliceLimitsRule())

	// Podman checks (AMD GPU)
	ROCmRegistry.Register(platform.NewROCmPlatformRule())
	ROCmRegistry.Register(power.NewPowerRule())
	// TODO: Register a ROCm-only rule validating that the Power system firmware level is FW1120 or later.
	ROCmRegistry.Register(rhn.NewRHNRule())
	ROCmRegistry.Register(amdgpu.NewAMDGPURule())
	ROCmRegistry.Register(amdgpu.NewROCmVersionRule())
	// memlock ulimits are required for GPU DMA; slice limits apply regardless of accelerator type.
	ROCmRegistry.Register(ulimits.NewUlimitsRule())
	ROCmRegistry.Register(slicelimits.NewSliceLimitsRule())

	// OpenshiftChecks
	OpenshiftRegistry.Register(kubeconfig.NewKubeconfigRule())
	OpenshiftRegistry.Register(nodelabels.NewNodeLabelsRule())
	OpenshiftRegistry.Register(operators.NewOperatorRule())
	OpenshiftRegistry.Register(spyrepolicy.NewSpyrePolicyRule())
	OpenshiftRegistry.Register(rhods.NewDSCInitializationRule())
	OpenshiftRegistry.Register(rhods.NewDataScienceClusterRule())
	OpenshiftRegistry.Register(storageclass.NewStorageClassRule())
}

// Rule defines the interface for validation rules.
type Rule interface {
	Verify(ctx context.Context) error
	Message() string
	Name() string
	Level() constants.ValidationLevel
	Hint() string
	Description() string
}

// PodmanRegistry is the default Podman registry for Spyre-based deployments.
var PodmanRegistry = NewValidationRegistry()

// ROCmRegistry is the Podman registry for AMD GPU-based deployments.
var ROCmRegistry = NewValidationRegistry()

var OpenshiftRegistry = NewValidationRegistry()

// ValidationRegistry holds the list of checks.
type ValidationRegistry struct {
	mu    sync.RWMutex
	rules []Rule
}

// NewValidationRegistry creates a new registry.
func NewValidationRegistry() *ValidationRegistry {
	return &ValidationRegistry{
		rules: make([]Rule, 0),
	}
}

// Register adds a new check to the list.
func (r *ValidationRegistry) Register(rule Rule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rules = append(r.rules, rule)
}

// Rules returns the list of registered checks.
func (r *ValidationRegistry) Rules() []Rule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.rules
}
