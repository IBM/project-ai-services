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

const (
	dscVersion = "v2"
	dscKind    = "DataScienceCluster"
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

// Verify checks if DataScienceCluster is in ready phase.
func (r *DataScienceCluster) Verify() error {
	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}
	gvk := schema.GroupVersionKind{
		Group:   strings.ToLower(dscKind) + ".opendatahub.io",
		Version: dscVersion,
		Kind:    dscKind,
	}

	obj, exists, err := utils.GetExistingCustomResource(client, gvk)
	if err != nil {
		return fmt.Errorf("failed to get existing DataScienceCluster: %w", err)
	}
	if !exists {
		return fmt.Errorf("DataScienceCluster not found")
	}

	phase, found, err := unstructured.NestedString(obj.Object, "status", "phase")
	if err != nil {
		return fmt.Errorf("failed to parse status.phase from dsc: %w", err)
	}

	if !found {
		return fmt.Errorf("DataScienceCluster status.phase not found")
	}

	if phase != "Ready" {
		return fmt.Errorf("\nDataScienceCluster not ready (status.phase: %s)", phase)
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
