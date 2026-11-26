package openshift

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	apiyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	yamlDecoderBufSz = 4096
)

var (
	// cachedSubscriptionList holds the subscription list to avoid multiple API calls.
	cachedSubscriptionList *operatorsv1alpha1.SubscriptionList
)

func applyYaml(ctx context.Context, c *openshift.OpenshiftClient, yaml []byte, skipUpgrade bool) error {
	resourceList := []*unstructured.Unstructured{}

	decoder := apiyaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(yaml)), yamlDecoderBufSz)
	for {
		resource := unstructured.Unstructured{}
		err := decoder.Decode(&resource)
		if err == nil {
			// Skip empty resources
			if resource.GetKind() != "" {
				resourceList = append(resourceList, &resource)
			}
		} else if err == io.EOF {
			break
		} else {
			return fmt.Errorf("error decoding to unstructured %v", err.Error())
		}
	}

	// Fetch subscription list once before processing resources
	if err := loadSubscriptionList(ctx, c); err != nil {
		return fmt.Errorf("failed to load subscription list: %v", err)
	}

	for _, object := range resourceList {
		if err := applyObject(ctx, c, object, skipUpgrade); err != nil {
			return fmt.Errorf("error applying object %v", err.Error())
		}
	}

	return nil
}

// loadSubscriptionList fetches and caches the subscription list if not already loaded.
func loadSubscriptionList(ctx context.Context, c *openshift.OpenshiftClient) error {
	if cachedSubscriptionList == nil {
		cachedSubscriptionList = &operatorsv1alpha1.SubscriptionList{}
		err := c.Client.List(ctx, cachedSubscriptionList)
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to list subscriptions: %w", err)
		}
		if apierrors.IsForbidden(err) {
			return fmt.Errorf("missing required permissions to list subscriptions")
		}
	}

	return nil
}

// applyObject applies the desired object against the apiserver.
func applyObject(ctx context.Context, c *openshift.OpenshiftClient, object *unstructured.Unstructured, skipUpgrade bool) error {
	// Retrieve name, namespace, groupVersionKind from given object.
	name := object.GetName()
	namespace := object.GetNamespace()
	if name == "" {
		return fmt.Errorf("object %s has no name", object.GroupVersionKind().String())
	}

	groupVersionKind := object.GroupVersionKind()
	kind := groupVersionKind.Kind

	// Pre-apply handling based on resource kind
	skip, err := handlePreApply(ctx, c, object, kind, skipUpgrade)
	if err != nil {
		return err
	}
	if skip {
		return nil
	}

	objDesc := fmt.Sprintf("(%s) %s/%s", groupVersionKind.String(), namespace, name)

	// Apply the k8s object with provided version kind in given namespace.
	err = c.Client.Apply(ctx, client.ApplyConfigurationFromUnstructured(object), &client.ApplyOptions{FieldManager: constants.AIServices, Force: utils.BoolPtr(true)})
	if err != nil {
		if apierrors.IsForbidden(err) {
			return fmt.Errorf("missing required permissions to create %s", objDesc)
		}

		return fmt.Errorf("could not create %s. Error: %v", objDesc, err.Error())
	}

	// Post-apply handling based on resource kind
	return handlePostApply(ctx, c, object, kind, namespace)
}

// handlePreApply performs pre-apply checks and modifications based on resource kind.
// Returns true if the resource should be skipped.
func handlePreApply(ctx context.Context, c *openshift.OpenshiftClient, object *unstructured.Unstructured, kind string, skipUpgrade bool) (bool, error) {
	switch kind {
	case "Subscription":
		packageName, _, _ := unstructured.NestedString(object.Object, "spec", "name")
		operatorLabel := getOperatorLabel(packageName)
		s := spinner.New(fmt.Sprintf("Applying %s configuration", operatorLabel))
		s.Start(ctx)

		return shouldSkipSubscription(ctx, c, object, skipUpgrade, s, operatorLabel)

	case "DSCInitialization", "DataScienceCluster":
		// Handle single-instance RHODS resources
		if shouldSkipOrUpdateRHODSResource(ctx, c, object) {
			return true, nil
		}
	}

	return false, nil
}

// handlePostApply performs post-apply actions based on resource kind.
func handlePostApply(ctx context.Context, c *openshift.OpenshiftClient, object *unstructured.Unstructured, kind, namespace string) error {
	switch kind {
	case "Subscription":
		return handleSubscriptionPostApply(ctx, c, object, namespace)
	default:
		return nil
	}
}

// handleSubscriptionPostApply waits for an operator to be ready after subscription is applied.
func handleSubscriptionPostApply(ctx context.Context, c *openshift.OpenshiftClient, object *unstructured.Unstructured, namespace string) error {
	packageName, found, err := unstructured.NestedString(object.Object, "spec", "name")
	if err != nil || !found || packageName == "" {
		return nil
	}

	targetCSV, _, _ := unstructured.NestedString(object.Object, "spec", "startingCSV")
	previousCSV := getPreviousInstalledCSV(packageName)
	operatorLabel := getOperatorLabel(packageName)

	s := spinner.New(fmt.Sprintf("Waiting for %s to be ready", operatorLabel))
	s.Start(ctx)

	if err := waitForOperator(ctx, c, packageName, namespace, targetCSV, previousCSV); err != nil {
		s.Fail(fmt.Sprintf("%s is not ready", operatorLabel))

		return fmt.Errorf("operator %s not ready: %w", packageName, err)
	}

	s.Stop(fmt.Sprintf("%s is up and ready", operatorLabel))

	return nil
}

// getPreviousInstalledCSV looks up the previously installed CSV from the cached subscription list.
func getPreviousInstalledCSV(packageName string) string {
	if cachedSubscriptionList == nil {
		return ""
	}

	for i := range cachedSubscriptionList.Items {
		sub := &cachedSubscriptionList.Items[i]
		if sub.Spec.Package == packageName {
			return sub.Status.InstalledCSV
		}
	}

	return ""
}

// isCSVReady reports whether the subscription's installed CSV is in Succeeded phase.
func isCSVReady(ctx context.Context, c *openshift.OpenshiftClient, sub *operatorsv1alpha1.Subscription) bool {
	if sub.Status.InstalledCSV == "" {
		return false
	}

	csv := &operatorsv1alpha1.ClusterServiceVersion{}
	err := c.Client.Get(ctx, client.ObjectKey{
		Name:      sub.Status.InstalledCSV,
		Namespace: sub.Namespace,
	}, csv)

	return err == nil && csv.Status.Phase == operatorsv1alpha1.CSVPhaseSucceeded
}

// handleExistingSubscription decides what to do when a matching subscription is
// already present on the cluster. Returns true if the caller should skip the apply.
func handleExistingSubscription(ctx context.Context, c *openshift.OpenshiftClient, sub *operatorsv1alpha1.Subscription, object *unstructured.Unstructured, skipUpgrade bool, s *spinner.Spinner, operatorLabel string) (bool, error) {
	// CSV hasn't landed yet — skip silently.
	if sub.Status.InstalledCSV == "" {
		s.Stop(fmt.Sprintf("%s is already installed and ready", operatorLabel))

		return true, nil
	}

	csvReady := isCSVReady(ctx, c, sub)

	if !skipUpgrade && csvReady {
		// promptAndUpgrade returns true when an upgrade was confirmed —
		// in that case the caller should NOT skip so waitForOperator picks up the new CSV.
		applied, err := promptAndUpgrade(sub, object, operatorLabel, s)
		if err != nil {
			return false, err
		}

		return !applied, nil
	}

	if csvReady {
		s.Stop(fmt.Sprintf("%s is already installed and ready", operatorLabel))
	}

	return true, nil
}

// shouldSkipSubscription checks if a subscription should be skipped because it already exists.
// Unless skipUpgrade is set, when the operator is ready the user is prompted to upgrade.
// Returns true if the resource should be skipped (i.e. no SSA apply needed).
func shouldSkipSubscription(ctx context.Context, c *openshift.OpenshiftClient, object *unstructured.Unstructured, skipUpgrade bool, s *spinner.Spinner, operatorLabel string) (bool, error) {
	packageName, found, err := unstructured.NestedString(object.Object, "spec", "name")
	if err != nil || !found || packageName == "" || cachedSubscriptionList == nil {
		s.Stop(fmt.Sprintf("Applying %s configuration", operatorLabel))

		return false, nil
	}

	for i := range cachedSubscriptionList.Items {
		sub := &cachedSubscriptionList.Items[i]
		if sub.Spec.Package != packageName {
			continue
		}

		return handleExistingSubscription(ctx, c, sub, object, skipUpgrade, s, operatorLabel)
	}

	s.Stop(fmt.Sprintf("Applying %s configuration", operatorLabel))

	return false, nil
}

// fetchSubscriptionByPackage finds a subscription in the given namespace matching the package name.
func fetchSubscriptionByPackage(ctx context.Context, c *openshift.OpenshiftClient, packageName string, opNS string) (*operatorsv1alpha1.Subscription, error) {
	subList := &operatorsv1alpha1.SubscriptionList{}
	if err := c.Client.List(ctx, subList, client.InNamespace(opNS)); err != nil {
		if apierrors.IsForbidden(err) {
			return nil, fmt.Errorf("missing required permissions to list subscriptions")
		}

		return nil, err
	}

	for i := range subList.Items {
		if subList.Items[i].Spec.Package == packageName {
			return &subList.Items[i], nil
		}
	}

	return nil, apierrors.NewNotFound(operatorsv1alpha1.Resource("subscription"), packageName)
}

// fetchOperatorByPackage fetches the CSV for an operator by package name.
func fetchOperatorByPackage(ctx context.Context, c *openshift.OpenshiftClient, packageName string, opNS string) (*operatorsv1alpha1.ClusterServiceVersion, error) {
	sub, err := fetchSubscriptionByPackage(ctx, c, packageName, opNS)
	if err != nil {
		return nil, err
	}

	// Use installedCSV from status instead of startingCSV from spec
	if sub.Status.InstalledCSV == "" {
		return nil, apierrors.NewNotFound(operatorsv1alpha1.Resource("clusterserviceversion"), "")
	}

	csv := &operatorsv1alpha1.ClusterServiceVersion{}
	if err := c.Client.Get(ctx, client.ObjectKey{
		Name:      sub.Status.InstalledCSV,
		Namespace: opNS,
	}, csv); err != nil {
		if apierrors.IsForbidden(err) {
			return nil, fmt.Errorf("missing required permissions to get ClusterServiceVersion")
		}

		return nil, err
	}

	return csv, nil
}

// waitForOperator waits for an operator to be ready after installation or upgrade.
func waitForOperator(ctx context.Context, c *openshift.OpenshiftClient, packageName, opNS, targetCSV, previousCSV string) error {
	return wait.PollUntilContextTimeout(ctx, constants.OperatorPollInterval, constants.OperatorPollTimeout, true, func(pollCtx context.Context) (bool, error) {
		sub, err := fetchSubscriptionByPackage(pollCtx, c, packageName, opNS)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// keep waiting until timeout
				return false, nil
			}
			if apierrors.IsForbidden(err) {
				return false, fmt.Errorf("missing required permissions to list subscriptions")
			}

			return false, err
		}

		if !isTargetCSVReached(sub.Status.InstalledCSV, targetCSV, previousCSV) {
			return false, nil
		}

		csv := &operatorsv1alpha1.ClusterServiceVersion{}
		if err := c.Client.Get(pollCtx, client.ObjectKey{
			Name:      sub.Status.InstalledCSV,
			Namespace: opNS,
		}, csv); err != nil {
			if apierrors.IsNotFound(err) {
				// keep waiting until timeout
				return false, nil
			}
			if apierrors.IsForbidden(err) {
				return false, fmt.Errorf("missing required permissions to get ClusterServiceVersion")
			}

			return false, err
		}

		// Check if CSV is in Succeeded phase
		if csv.Status.Phase == operatorsv1alpha1.CSVPhaseSucceeded {
			return true, nil
		}

		return false, nil
	})
}

// getOperatorLabel returns the label for a given package name from constants.
func getOperatorLabel(packageName string) string {
	for _, op := range constants.RequiredOperators {
		pkgName := op.Package
		if pkgName == "" {
			pkgName = op.Name
		}
		if pkgName == packageName {
			return op.Label
		}
	}

	return packageName
}

// shouldSkipOrUpdateRHODSResource handles single-instance RHODS resources (DSC, DSCI).
// Returns true if the resource should be skipped.
func shouldSkipOrUpdateRHODSResource(ctx context.Context, c *openshift.OpenshiftClient, object *unstructured.Unstructured) bool {
	kind := object.GetKind()

	// Check if resource already exists
	gvk := schema.GroupVersionKind{
		Group:   strings.ToLower(kind) + ".opendatahub.io",
		Version: constants.VersionV2,
		Kind:    kind,
	}
	existingResource, exists, err := utils.GetExistingCustomResource(ctx, c, gvk)
	if err != nil {
		logger.Debugf("Error checking for existing %s: %v", kind, err)

		return false
	}

	if !exists {
		// Resource doesn't exist, proceed with creation
		return false
	}

	existingName := existingResource.GetName()
	logger.Debugf("Found existing %s named '%s'", kind, existingName)

	// Check if resource has re-apply annotation set to false
	annotations := object.GetAnnotations()
	if annotations != nil {
		if reApply, ok := annotations["ai-services.io/re-apply"]; ok && reApply == "false" {
			logger.Debugf("Skipping %s as re-apply annotation is set to false", kind)

			return true
		}
	}

	// Update the object name to match existing resource
	object.SetName(existingName)

	return false
}
