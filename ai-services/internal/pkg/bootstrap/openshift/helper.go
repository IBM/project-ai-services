package openshift

import (
	"context"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	OLMGroup       = "operators.coreos.com"
	OLMVersion     = "v1alpha1"
	OLMCSVList     = "ClusterServiceVersionList"
	PhaseSucceeded = "Succeeded"
)

/* ---------- Operator Validation ---------- */

func ValidateOperator(ctx context.Context, operatorSubstring string) error {
	csvList := &unstructured.UnstructuredList{}

	csvList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   OLMGroup,
		Version: OLMVersion,
		Kind:    OLMCSVList,
	})

	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	if err := client.Client.List(ctx, csvList); err != nil {
		return fmt.Errorf("failed to list ClusterServiceVersions: %w", err)
	}

	for _, csv := range csvList.Items {
		name := csv.GetName()

		if !strings.HasPrefix(name, operatorSubstring+".") {
			continue
		}

		phase, _, _ := unstructured.NestedString(csv.Object, "status", "phase")

		if phase == PhaseSucceeded {
			return nil
		}

		return fmt.Errorf("operator %s found but not ready (phase=%s)", name, phase)
	}

	return fmt.Errorf("operator not installed: %s", operatorSubstring)
}
