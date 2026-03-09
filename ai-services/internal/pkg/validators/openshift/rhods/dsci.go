package rhods

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
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

// Verify performs a direct fetch.
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

	return wait.PollUntilContextTimeout(client.Ctx, constants.OperatorPollInterval, constants.OperatorPollTimeout, true, func(ctx context.Context) (bool, error) {
		if err := client.Client.Get(ctx, types.NamespacedName{Name: dsciName}, obj); err != nil {
			if apierrors.IsNotFound(err) {
				logger.Infof("DSCInitialization %s not found yet, retrying...", dsciName, logger.VerbosityLevelDebug)

				return false, nil
			}

			return false, fmt.Errorf("failed to find %s: %w", dsciName, err)
		}

		phase, found, err := unstructured.NestedString(obj.Object, "status", "phase")
		if err != nil {
			return false, fmt.Errorf("failed to parse status.phase from dsci: %w", err)
		}

		if !found || phase != "Ready" {
			if !found {
				phase = "unknown"
			}
			logger.Infof("DSCInitialization not ready yet (status.phase: %s), waiting...", phase, logger.VerbosityLevelDebug)

			return false, nil
		}
		logger.Infof("DSCInitialization %s is ready", dsciName, logger.VerbosityLevelDebug)

		return true, nil
	})
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
