package mustgather

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// MustGatherOptions contains options for the must-gather command.
type MustGatherOptions struct {
	OutputDir       string
	ApplicationName string
}

// MustGatherer is the interface for gathering debugging information.
type MustGatherer interface {
	Gather(opts MustGatherOptions) error
}

// MustGatherFactory creates MustGatherer instances based on runtime type.
type MustGatherFactory struct {
	runtimeType types.RuntimeType
}

// NewMustGatherFactory creates a new MustGatherFactory.
func NewMustGatherFactory(rt types.RuntimeType) *MustGatherFactory {
	return &MustGatherFactory{
		runtimeType: rt,
	}
}

// Create creates a MustGatherer instance based on the runtime type.
func (f *MustGatherFactory) Create() (MustGatherer, error) {
	switch f.runtimeType {
	case types.RuntimeTypePodman:
		return NewPodmanMustGatherer(), nil
	case types.RuntimeTypeOpenShift:
		return NewOpenShiftMustGatherer(), nil
	default:
		return nil, fmt.Errorf("unsupported runtime type: %s", f.runtimeType)
	}
}

// Made with Bob
