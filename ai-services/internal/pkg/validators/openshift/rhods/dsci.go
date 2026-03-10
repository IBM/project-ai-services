package rhods

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

const (
	dsciGroup   = "dscinitialization.opendatahub.io"
	dsciVersion = "v2"
	dsciKind    = "DSCInitialization"
	dsciName    = "default-dsci"
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

// Verify performs a direct check without polling.
func (r *DSCInitialization) Verify() error {
	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   dsciGroup,
		Version: dsciVersion,
		Kind:    dsciKind,
	})

	if err := client.Client.Get(client.Ctx, types.NamespacedName{Name: dsciName}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("DSCInitialization %s not found", dsciName)
		}

		return fmt.Errorf("failed to find %s: %w", dsciName, err)
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
