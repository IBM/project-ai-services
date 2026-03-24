package operators

import (
	"fmt"
	"strings"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8sClient "sigs.k8s.io/controller-runtime/pkg/client"
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

	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	// Prefetch all subscriptions across all namespaces
	allSubscriptions := &operatorsv1alpha1.SubscriptionList{}
	if err := client.Client.List(client.Ctx, allSubscriptions); err != nil {
		return fmt.Errorf("failed to list subscriptions: %w", err)
	}

	for _, op := range constants.RequiredOperators {
		opPackage := op.Package
		if opPackage == "" {
			opPackage = op.Name
		}
		if err := validateOperatorByPackage(client, opPackage, op.Namespace, allSubscriptions); err != nil {
			failed = append(failed, fmt.Sprintf("  - %s: %s", op.Label, err.Error()))
		} else {
			r.passed = append(r.passed, fmt.Sprintf("  - %s installed", op.Label))
		}
	}

	if len(failed) > 0 {
		return fmt.Errorf("operator validation failed: \n%s", strings.Join(append(r.passed, failed...), "\n"))
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
	return "This tool requires certain operators to be up and running, please run `ai-services bootstrap configure --runtime openshift` to install required operators"
}

func validateOperatorByPackage(c *openshift.OpenshiftClient, packageName, opNamespace string, allSubscriptions *operatorsv1alpha1.SubscriptionList) error {
	// Find subscription with matching package name in the specified namespace
	var sub *operatorsv1alpha1.Subscription
	for i := range allSubscriptions.Items {
		if allSubscriptions.Items[i].Spec.Package == packageName && allSubscriptions.Items[i].Namespace == opNamespace {
			sub = &allSubscriptions.Items[i]

			break
		}
	}

	if sub == nil {
		return fmt.Errorf("subscription with package '%s' not found", packageName)
	}

	// Check if CSV is installed
	if sub.Status.InstalledCSV == "" {
		return fmt.Errorf("no CSV installed yet")
	}

	// Get CSV
	csv := &operatorsv1alpha1.ClusterServiceVersion{}
	if err := c.Client.Get(c.Ctx, k8sClient.ObjectKey{
		Name:      sub.Status.InstalledCSV,
		Namespace: opNamespace,
	}, csv); err != nil {
		if apierrors.IsNotFound(err) {
			return fmt.Errorf("CSV not found")
		}

		return fmt.Errorf("failed to get CSV: %w", err)
	}

	// Check CSV phase
	if csv.Status.Phase != operatorsv1alpha1.CSVPhaseSucceeded {
		return fmt.Errorf("not ready (phase: %s)", csv.Status.Phase)
	}

	return nil
}
