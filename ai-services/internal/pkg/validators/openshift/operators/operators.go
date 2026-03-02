package operators

import (
	"context"
	"fmt"
	"strings"

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
	secondarySchedulerOperator = "secondaryscheduleroperator"
	certManagerOperator        = "cert-manager-operator"
	serviceMeshOperator        = "servicemeshoperator3"
	nfdOperator                = "nfd"
	rhoaiOperator              = "rhods-operator"
	olmGroup                   = "operators.coreos.com"
	olmVersion                 = "v1alpha1"
	olmCSVList                 = "ClusterServiceVersionList"
	phaseSucceeded             = "Succeeded"
)

type OperatorRule struct {
	passed []string
}

func NewOperatorRule() *OperatorRule {
	return &OperatorRule{}
}

func (r *OperatorRule) Name() string {
	return "operators"
}

func (r *OperatorRule) Description() string {
	return "Validates that all operators are installed or not"
}

func (r *OperatorRule) Verify() error {
	var failed []string

	checks := []struct {
		name     string
		operator string
	}{
		{
			"Secondary Scheduler Operator",
			secondarySchedulerOperator,
		},
		{
			"Cert-Manager Operator",
			certManagerOperator,
		},
		{
			"Service Mesh 3 Operator",
			serviceMeshOperator,
		},
		{
			"Node Feature Discovery Operator",
			nfdOperator,
		},
		{
			"RHOAI Operator",
			rhoaiOperator,
		},
	}

	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	csvList := &unstructured.UnstructuredList{}
	csvList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   olmGroup,
		Version: olmVersion,
		Kind:    olmCSVList,
	})
	if err := client.Client.List(client.Ctx, csvList); err != nil {
		return fmt.Errorf("failed to list ClusterServiceVersions: %w", err)
	}

	for _, check := range checks {
		if err := validateOperator(client, csvList, check.operator); err != nil {
			failed = append(failed, fmt.Sprintf("  - %s: %s", check.name, err.Error()))
		} else {
			r.passed = append(r.passed, fmt.Sprintf("  - %s installed", check.name))
		}
	}

	if len(failed) > 0 {
		checks := append(r.passed, failed...)

		return fmt.Errorf("operator validation failed: \n%s", strings.Join(checks, "\n"))
	}

	return nil
}

func (r *OperatorRule) Message() string {
	return "Operators installed\n" + strings.Join(r.passed, "\n")
}

func (r *OperatorRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *OperatorRule) Hint() string {
	return "This tool requires certain operators to be up and running, please run `ai-services bootstrap configure` to install required operators"
}

func validateOperator(c *openshift.OpenshiftClient, csvList *unstructured.UnstructuredList, operatorSubstring string) error {
	for _, csv := range csvList.Items {
		name := csv.GetName()
		if !strings.HasPrefix(name, operatorSubstring+".") {
			continue
		}

		// operator found, wait until it is ready
		return wait.PollUntilContextTimeout(c.Ctx, constants.OperatorpPollInterval, constants.OperatorpPollTimeout, true, func(ctx context.Context) (done bool, err error) {
			current := &unstructured.Unstructured{}
			current.SetGroupVersionKind(csv.GroupVersionKind())

			if err := c.Client.Get(ctx, types.NamespacedName{
				Name:      name,
				Namespace: csv.GetNamespace(),
			}, current); err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}

			phase, _, _ := unstructured.NestedString(current.Object, "status", "phase")
			if phase == phaseSucceeded {
				return true, nil
			}
			logger.Infof("Operator %s not ready yet (phase: %s), waiting...", name, phase, logger.VerbosityLevelDebug)

			return false, nil
		})
	}

	return fmt.Errorf("operator not installed: %s", operatorSubstring)
}
