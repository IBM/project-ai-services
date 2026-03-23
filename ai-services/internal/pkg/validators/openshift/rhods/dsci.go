package rhods

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type DSCInitialization struct{}

func NewDSCInitializationRule() *DSCInitialization {
	return &DSCInitialization{}
}

func (r *DSCInitialization) Name() string {
	return "dsci"
}

func (r *DSCInitialization) Description() string {
	return "Validates that DSC Initialization is in ready state"
}

// Verify checks DSCInitialization is in Ready phase.
func (r *DSCInitialization) Verify() error {
	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}
	gvk := schema.GroupVersionKind{
		Group:   strings.ToLower(constants.DSCIKind) + ".opendatahub.io",
		Version: constants.VersionV2,
		Kind:    constants.DSCIKind,
	}

	obj, exists, err := utils.GetExistingCustomResource(client, gvk)
	if err != nil {
		return fmt.Errorf("failed to get existing DSCInitialization: %w", err)
	}
	if !exists {
		return fmt.Errorf("DSCInitialization not found")
	}

	phase, found, err := unstructured.NestedString(obj.Object, "status", "phase")
	if err != nil {
		return fmt.Errorf("failed to parse status.phase from dsci: %w", err)
	}

	if !found {
		return fmt.Errorf("DSCInitialization status.phase not found")
	}

	if phase != "Ready" {
		return fmt.Errorf("DSCInitialization not ready (status.phase: %s)", phase)
	}

	return nil
}

func (r *DSCInitialization) Message() string {
	return "DSC Initialization is ready"
}

func (r *DSCInitialization) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *DSCInitialization) Hint() string {
	return "Run 'oc get DSCInitialization and ensure status.phase is 'Ready'."
}
