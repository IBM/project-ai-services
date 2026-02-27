package validators

import (
	"sync"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	kubeconfig "github.com/project-ai-services/ai-services/internal/pkg/validators/openshift/kubeconfig"
	operators "github.com/project-ai-services/ai-services/internal/pkg/validators/openshift/operators"
	spyrepolicy "github.com/project-ai-services/ai-services/internal/pkg/validators/openshift/spyrepolicy"
	storageclass "github.com/project-ai-services/ai-services/internal/pkg/validators/openshift/storageclass"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/numa"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/platform"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/power"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/rhn"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/root"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/servicereport"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/spyre"
)

// Initialize the default registry with built-in rules.
func init() {
	// Podman checks
	// adding root rule on top to verify this check first
	PodmanRegistry.Register(root.NewRootRule())
	PodmanRegistry.Register(numa.NewNumaRule())
	PodmanRegistry.Register(platform.NewPlatformRule())
	PodmanRegistry.Register(power.NewPowerRule())
	PodmanRegistry.Register(rhn.NewRHNRule())
	PodmanRegistry.Register(spyre.NewSpyreRule())
	PodmanRegistry.Register(servicereport.NewServiceReportRule())

	// OpenshiftChecks
	OpenshiftRegistry.Register(kubeconfig.NewKubeconfigRule())
	OpenshiftRegistry.Register(operators.NewOperatorRule())
	OpenshiftRegistry.Register(spyrepolicy.NewSpyrePolicyRule())
	OpenshiftRegistry.Register(storageclass.NewStorageClassRule())
}

// Rule defines the interface for validation rules.
type Rule interface {
	Verify() error
	Message() string
	Name() string
	Level() constants.ValidationLevel
	Hint() string
	Description() string
}

// PodmanRegistry is the podman registry instance that holds all registered checks.
var PodmanRegistry = NewValidationRegistry()
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
