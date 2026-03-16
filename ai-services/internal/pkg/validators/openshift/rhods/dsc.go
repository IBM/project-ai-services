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
	dscGroup   = "datasciencecluster.opendatahub.io"
	dscVersion = "v2"
	dscKind    = "DataScienceCluster"
	dscName    = "default-dsc"
)

type DataScienceCluster struct{}

func NewDataScienceClusterRule() *DataScienceCluster {
	return &DataScienceCluster{}
}

func (r *DataScienceCluster) Name() string {
	return "dsc"
}

func (r *DataScienceCluster) Description() string {
	return "Validates that Data Science Cluster is in ready phase"
}

// Verify performs a direct check without polling.
func (r *DataScienceCluster) Verify() error {
	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   dscGroup,
		Version: dscVersion,
		Kind:    dscKind,
	})

	if err := client.Client.Get(client.Ctx, types.NamespacedName{Name: dscName}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("DataScienceCluster %s not found", dscName)
		}

		return fmt.Errorf("failed to find %s: %w", dscName, err)
	}

	phase, found, err := unstructured.NestedString(obj.Object, "status", "phase")
	if err != nil {
		return fmt.Errorf("failed to parse status.phase from dsc: %w", err)
	}

	if !found {
		return fmt.Errorf("DataScienceCluster status.phase not found")
	}

	if phase != "Ready" {
		return fmt.Errorf("DataScienceCluster not ready (status.phase: %s)", phase)
	}

	return nil
}

func (r *DataScienceCluster) Message() string {
	return "Data Science Cluster is ready"
}

func (r *DataScienceCluster) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *DataScienceCluster) Hint() string {
	return "Run 'oc get DataScienceCluster and ensure status.phase is 'Ready'."
}
