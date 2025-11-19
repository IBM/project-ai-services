package validators

import (
	"sync"

	"github.com/project-ai-services/ai-services/internal/pkg/validators/numa"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/platform"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/power"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/rhn"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/root"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/spyre"
)

// Initialize the default registry with built-in rules
func init() {
	DefaultRegistry.Register(numa.NewNumaRule())
	DefaultRegistry.Register(platform.NewPlatformRule())
	DefaultRegistry.Register(power.NewPowerRule())
	DefaultRegistry.Register(rhn.NewRHNRule())
	DefaultRegistry.Register(root.NewRootRule())
	DefaultRegistry.Register(spyre.NewSpyreRule())
}

// Rule defines the interface for validation rules
type Rule interface {
	Verify() error
	Message() string
	Name() string
}

// DefaultRegistry is the default registry instance that holds all registered checks.
var DefaultRegistry = NewValidationRegistry()

// CheckRegistry holds the list of checks.
type ValidationRegistry struct {
	mu    sync.RWMutex
	rules []Rule
}

// NewCheckRegistry creates a new registry.
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
