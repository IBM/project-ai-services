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
	s *spinner.Spinner
	// cachedSubscriptionList holds the subscription list to avoid multiple API calls.
	cachedSubscriptionList *operatorsv1alpha1.SubscriptionList
)

func applyYaml(c *openshift.OpenshiftClient, yaml []byte) error {
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
	if err := loadSubscriptionList(c); err != nil {
		return fmt.Errorf("failed to load subscription list: %v", err)
	}

	for _, object := range resourceList {
		if err := applyObject(c, object); err != nil {
			return fmt.Errorf("error applying object %v", err.Error())
		}
	}

	return nil
}

// loadSubscriptionList fetches and caches the subscription list if not already loaded.
func loadSubscriptionList(c *openshift.OpenshiftClient) error {
	if cachedSubscriptionList == nil {
		cachedSubscriptionList = &operatorsv1alpha1.SubscriptionList{}
		err := c.Client.List(c.Ctx, cachedSubscriptionList)
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("failed to list subscriptions: %w", err)
		}
	}

	return nil
}

// applyObject applies the desired object against the apiserver.
func applyObject(c *openshift.OpenshiftClient, object *unstructured.Unstructured) error {
	// Retrieve name, namespace, groupVersionKind from given object.
	name := object.GetName()
	namespace := object.GetNamespace()
	if name == "" {
		return fmt.Errorf("object %s has no name", object.GroupVersionKind().String())
	}

	groupVersionKind := object.GroupVersionKind()
	kind := groupVersionKind.Kind

	// Pre-apply handling based on resource kind
	skip, err := handlePreApply(c, object, kind)
	if err != nil {
		return err
	}
	if skip {
		return nil
	}

	objDesc := fmt.Sprintf("(%s) %s/%s", groupVersionKind.String(), namespace, name)

	// Apply the k8s object with provided version kind in given namespace.
	err = c.Client.Apply(c.Ctx, client.ApplyConfigurationFromUnstructured(object), &client.ApplyOptions{FieldManager: constants.AIServices})
	if err != nil {
		return fmt.Errorf("could not create %s. Error: %v", objDesc, err.Error())
	}

	// Post-apply handling based on resource kind
	return handlePostApply(c, object, kind, namespace)
}

// handlePreApply performs pre-apply checks and modifications based on resource kind.
// Returns true if the resource should be skipped.
func handlePreApply(c *openshift.OpenshiftClient, object *unstructured.Unstructured, kind string) (bool, error) {
	switch kind {
	case "Subscription":
		s = spinner.New("Applying operator configurations")
		s.Start(c.Ctx)

		if shouldSkipSubscription(c, object) {
			return true, nil
		}

	case "DSCInitialization", "DataScienceCluster":
		// Handle single-instance RHODS resources
		if shouldSkipOrUpdateRHODSResource(c, object) {
			return true, nil
		}
	}

	return false, nil
}

// handlePostApply performs post-apply actions based on resource kind.
func handlePostApply(c *openshift.OpenshiftClient, object *unstructured.Unstructured, kind, namespace string) error {
	switch kind {
	case "Subscription":
		return handleSubscriptionPostApply(c, object, namespace)
	default:
		return nil
	}
}

// handleSubscriptionPostApply waits for an operator to be ready after subscription is applied.
func handleSubscriptionPostApply(c *openshift.OpenshiftClient, object *unstructured.Unstructured, namespace string) error {
	packageName, found, err := unstructured.NestedString(object.Object, "spec", "name")
	if err != nil || !found || packageName == "" {
		return nil
	}

	operatorLabel := getOperatorLabel(packageName)
	if err := waitForOperator(c, packageName, namespace); err != nil {
		s.Fail(fmt.Sprintf("%s is not ready", operatorLabel))

		return fmt.Errorf("operator %s not ready: %w", packageName, err)
	}

	s.Stop(fmt.Sprintf("%s is up and ready", operatorLabel))

	return nil
}

// shouldSkipSubscription checks if a subscription should be skipped because it already exists.
// If it exists and is ready, prints a message. Returns true if the subscription should be skipped.
func shouldSkipSubscription(c *openshift.OpenshiftClient, object *unstructured.Unstructured) bool {
	packageName, found, err := unstructured.NestedString(object.Object, "spec", "name")
	if err != nil || !found || packageName == "" || cachedSubscriptionList == nil {
		return false
	}

	// Check if any subscription has the same package name
	for _, sub := range cachedSubscriptionList.Items {
		if sub.Spec.Package != packageName {
			continue
		}

		// Found existing subscription, check its status
		if sub.Status.InstalledCSV == "" {
			return true
		}

		// Get the CSV to check if it's ready
		csv := &operatorsv1alpha1.ClusterServiceVersion{}
		if err := c.Client.Get(c.Ctx, client.ObjectKey{
			Name:      sub.Status.InstalledCSV,
			Namespace: sub.Namespace,
		}, csv); err == nil && csv.Status.Phase == operatorsv1alpha1.CSVPhaseSucceeded {
			// Get operator label from constants
			operatorLabel := getOperatorLabel(packageName)
			s.Stop(fmt.Sprintf("%s is already installed and ready", operatorLabel))
		}

		return true
	}

	return false
}

// fetchOperatorByPackage fetches the CSV for an operator by package name.
func fetchOperatorByPackage(c *openshift.OpenshiftClient, packageName string, opNS string) (*operatorsv1alpha1.ClusterServiceVersion, error) {
	// List all subscriptions in the namespace
	subList := &operatorsv1alpha1.SubscriptionList{}
	if err := c.Client.List(c.Ctx, subList, client.InNamespace(opNS)); err != nil {
		return nil, err
	}

	// Find subscription with matching package name
	var sub *operatorsv1alpha1.Subscription
	for i := range subList.Items {
		if subList.Items[i].Spec.Package == packageName {
			sub = &subList.Items[i]

			break
		}
	}

	if sub == nil {
		return nil, apierrors.NewNotFound(operatorsv1alpha1.Resource("subscription"), packageName)
	}

	// Use installedCSV from status instead of startingCSV from spec
	if sub.Status.InstalledCSV == "" {
		return nil, apierrors.NewNotFound(operatorsv1alpha1.Resource("clusterserviceversion"), "")
	}

	csv := &operatorsv1alpha1.ClusterServiceVersion{}
	if err := c.Client.Get(c.Ctx, client.ObjectKey{
		Name:      sub.Status.InstalledCSV,
		Namespace: opNS,
	}, csv); err != nil {
		return nil, err
	}

	return csv, nil
}

// waitForOperator waits for an operator to be ready after installation.
func waitForOperator(c *openshift.OpenshiftClient, packageName string, opNS string) error {
	return wait.PollUntilContextTimeout(c.Ctx, constants.OperatorPollInterval, constants.OperatorPollTimeout, true, func(ctx context.Context) (bool, error) {
		csv, err := fetchOperatorByPackage(c, packageName, opNS)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// keep waiting until timeout
				return false, nil
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
func shouldSkipOrUpdateRHODSResource(c *openshift.OpenshiftClient, object *unstructured.Unstructured) bool {
	kind := object.GetKind()

	// Check if resource already exists
	existingResource, exists, err := getExistingRHODSResource(c, kind)
	if err != nil {
		logger.Infof("Error checking for existing %s: %v", kind, err, logger.VerbosityLevelDebug)

		return false
	}

	if !exists {
		// Resource doesn't exist, proceed with creation
		return false
	}

	existingName := existingResource.GetName()
	logger.Infof("Found existing %s named '%s'", kind, existingName, logger.VerbosityLevelDebug)

	// Check if resource has re-apply annotation set to false
	annotations := object.GetAnnotations()
	if annotations != nil {
		if reApply, ok := annotations["ai-services.io/re-apply"]; ok && reApply == "false" {
			logger.Infof("Skipping %s as re-apply annotation is set to false", kind, logger.VerbosityLevelDebug)

			return true
		}
	}

	// Update the object name to match existing resource
	object.SetName(existingName)

	return false
}

// getExistingRHODSResource checks if a single instance RHODS resource exists and returns the resource object.
func getExistingRHODSResource(c *openshift.OpenshiftClient, kind string) (*unstructured.Unstructured, bool, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   strings.ToLower(kind) + ".opendatahub.io",
		Version: "v2",
		Kind:    kind,
	})

	if err := c.Client.List(c.Ctx, list); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}

		return nil, false, fmt.Errorf("error listing %s: %w", kind, err)
	}

	if len(list.Items) == 0 {
		return nil, false, nil
	}

	return &list.Items[0], true, nil
}
