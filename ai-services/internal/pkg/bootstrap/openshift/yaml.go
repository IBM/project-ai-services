package openshift

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	semver "github.com/blang/semver/v4"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/lipgloss"
	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	apiyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// upgradeVersions holds the current and available version information for display.
type upgradeVersions struct {
	currentChannel   string
	currentCSV       string
	availableChannel string
	availableCSV     string
	// availableCSVLabel is availableCSV or "(latest in channel)" when unset.
	availableCSVLabel string
}

const (
	yamlDecoderBufSz   = 4096
	tableColumnPadding = 2
)

// csvVersionRe matches the semver portion of an OLM CSV name, e.g.
//
//	rhods-operator.3.5.0   → "3.5.0"
//	spyre-operator.v1.3.1  → "v1.3.1"
var csvVersionRe = regexp.MustCompile(`v?(\d+\.\d+\.\d+.*)$`)

// csvVersion extracts a comparable semver from an OLM CSV name.
// Returns a zero version and false if the name carries no recognisable semver.
func csvVersion(csvName string) (semver.Version, bool) {
	m := csvVersionRe.FindString(csvName)
	if m == "" {
		return semver.Version{}, false
	}

	v, err := semver.ParseTolerant(m)
	if err != nil {
		return semver.Version{}, false
	}

	return v, true
}

var (
	s *spinner.Spinner
	// cachedSubscriptionList holds the subscription list to avoid multiple API calls.
	cachedSubscriptionList *operatorsv1alpha1.SubscriptionList
)

func applyYaml(ctx context.Context, c *openshift.OpenshiftClient, yaml []byte) error {
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
		if err := applyObject(ctx, c, object); err != nil {
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
func applyObject(ctx context.Context, c *openshift.OpenshiftClient, object *unstructured.Unstructured) error {
	// Retrieve name, namespace, groupVersionKind from given object.
	name := object.GetName()
	namespace := object.GetNamespace()
	if name == "" {
		return fmt.Errorf("object %s has no name", object.GroupVersionKind().String())
	}

	groupVersionKind := object.GroupVersionKind()
	kind := groupVersionKind.Kind

	// Pre-apply handling based on resource kind
	skip, err := handlePreApply(ctx, c, object, kind)
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
func handlePreApply(ctx context.Context, c *openshift.OpenshiftClient, object *unstructured.Unstructured, kind string) (bool, error) {
	switch kind {
	case "Subscription":
		s = spinner.New("Applying operator configurations")
		s.Start(ctx)

		if shouldSkipSubscription(ctx, c, object) {
			return true, nil
		}

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

	operatorLabel := getOperatorLabel(packageName)
	if err := waitForOperator(ctx, c, packageName, namespace); err != nil {
		s.Fail(fmt.Sprintf("%s is not ready", operatorLabel))

		return fmt.Errorf("operator %s not ready: %w", packageName, err)
	}

	s.Stop(fmt.Sprintf("%s is up and ready", operatorLabel))

	return nil
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
func handleExistingSubscription(ctx context.Context, c *openshift.OpenshiftClient, sub *operatorsv1alpha1.Subscription, object *unstructured.Unstructured) bool {
	// CSV hasn't landed yet — skip silently.
	if sub.Status.InstalledCSV == "" {
		return true
	}

	csvReady := isCSVReady(ctx, c, sub)
	operatorLabel := getOperatorLabel(sub.Spec.Package)

	if upgradeMode && csvReady {
		// promptAndUpgrade returns true when an upgrade was confirmed and applied —
		// in that case the caller should NOT skip so waitForOperator picks up the new CSV.
		return !promptAndUpgrade(ctx, c, sub, object, operatorLabel)
	}

	if csvReady {
		s.Stop(fmt.Sprintf("%s is already installed and ready", operatorLabel))
	}

	return true
}

// shouldSkipSubscription checks if a subscription should be skipped because it already exists.
// When upgradeMode is true and the operator is ready, the user is prompted to upgrade.
// Returns true if the resource should be skipped (i.e. no SSA apply needed).
func shouldSkipSubscription(ctx context.Context, c *openshift.OpenshiftClient, object *unstructured.Unstructured) bool {
	packageName, found, err := unstructured.NestedString(object.Object, "spec", "name")
	if err != nil || !found || packageName == "" || cachedSubscriptionList == nil {
		return false
	}

	for i := range cachedSubscriptionList.Items {
		sub := &cachedSubscriptionList.Items[i]
		if sub.Spec.Package != packageName {
			continue
		}

		return handleExistingSubscription(ctx, c, sub, object)
	}

	return false
}

// extractUpgradeVersions pulls version fields from the subscription and desired manifest.
func extractUpgradeVersions(sub *operatorsv1alpha1.Subscription, desired *unstructured.Unstructured) upgradeVersions {
	desiredChannel, _, _ := unstructured.NestedString(desired.Object, "spec", "channel")
	desiredCSV, _, _ := unstructured.NestedString(desired.Object, "spec", "startingCSV")

	label := desiredCSV
	if label == "" {
		label = "(latest in channel)"
	}

	return upgradeVersions{
		currentChannel:    sub.Spec.Channel,
		currentCSV:        sub.Status.InstalledCSV,
		availableChannel:  desiredChannel,
		availableCSV:      desiredCSV,
		availableCSVLabel: label,
	}
}

// isDowngrade returns true when the available CSV is semver-older than the current one.
func isDowngrade(v upgradeVersions) bool {
	if v.availableCSV == "" {
		return false
	}

	currentVer, currentOK := csvVersion(v.currentCSV)
	availableVer, availableOK := csvVersion(v.availableCSV)

	return currentOK && availableOK && availableVer.LT(currentVer)
}

// printUpgradeTable renders a bubbles/table showing current vs available versions.
func printUpgradeTable(v upgradeVersions, operatorLabel string) {
	cols := []table.Column{
		{Title: ""},
		{Title: "CHANNEL"},
		{Title: "VERSION (CSV)"},
	}
	rows := []table.Row{
		{"Current  ", v.currentChannel, v.currentCSV},
		{"Available", v.availableChannel, v.availableCSVLabel},
	}

	// Compute column widths from content.
	widths := make([]int, len(cols))
	for i, col := range cols {
		widths[i] = len(col.Title)
	}
	for _, r := range rows {
		for i, cell := range r {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	for i := range cols {
		cols[i].Width = widths[i] + tableColumnPadding
	}

	tbl := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(len(rows)+1),
	)

	styles := table.DefaultStyles()
	styles.Header = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		Padding(0, 1).
		Bold(true)
	styles.Cell = lipgloss.NewStyle().Padding(0, 1)
	styles.Selected = lipgloss.NewStyle()
	tbl.SetStyles(styles)

	labelStyle := lipgloss.NewStyle().Bold(true)
	currentStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))   // yellow
	availableStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green

	logger.Infoln(labelStyle.Render("\nUpgrade available: " + operatorLabel))
	for _, line := range strings.Split(tbl.View(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(line, "Current") {
			logger.Infoln(currentStyle.Render(line))
		} else if strings.Contains(line, "Available") {
			logger.Infoln(availableStyle.Render(line))
		} else {
			logger.Infoln(line)
		}
	}
}

// promptAndUpgrade shows the current/available version table, prompts the user,
// and applies the subscription patch if confirmed.
// Returns true if the upgrade was applied (caller should proceed with waitForOperator),
// false if skipped (caller should treat the subscription as already handled).
func promptAndUpgrade(ctx context.Context, c *openshift.OpenshiftClient, sub *operatorsv1alpha1.Subscription, desired *unstructured.Unstructured, operatorLabel string) bool {
	v := extractUpgradeVersions(sub, desired)

	// Already at the desired version — nothing to do.
	if v.currentChannel == v.availableChannel && (v.availableCSV == "" || v.currentCSV == v.availableCSV) {
		s.Stop(fmt.Sprintf("%s is already at the desired version (%s / %s)", operatorLabel, v.currentChannel, v.currentCSV))

		return false
	}

	// Block downgrades silently.
	if isDowngrade(v) {
		s.Stop(fmt.Sprintf("%s: skipping, manifest version %s is older than installed %s",
			operatorLabel, v.availableCSV, v.currentCSV))

		return false
	}

	s.Stop(fmt.Sprintf("%s upgrade available", operatorLabel))
	printUpgradeTable(v, operatorLabel)

	confirmed, err := utils.ConfirmAction(fmt.Sprintf("\nUpgrade %s?", operatorLabel))
	if err != nil || !confirmed {
		logger.Infof("Skipping upgrade for %s", operatorLabel)

		return false
	}

	if err := upgradeSubscription(ctx, c, sub, v.availableChannel, v.availableCSV); err != nil {
		logger.Warningf("Failed to upgrade %s: %v", operatorLabel, err)

		return false
	}

	logger.Infof("Subscription for %s updated — waiting for new version to roll out", operatorLabel)

	return true
}

// upgradeSubscription patches an existing Subscription's channel and startingCSV
// so that OLM picks up the upgrade.
func upgradeSubscription(ctx context.Context, c *openshift.OpenshiftClient, sub *operatorsv1alpha1.Subscription, channel, startingCSV string) error {
	patch := fmt.Sprintf(`{"spec":{"channel":%q,"startingCSV":%q}}`, channel, startingCSV)

	existing := &operatorsv1alpha1.Subscription{}
	existing.Name = sub.Name
	existing.Namespace = sub.Namespace

	if err := c.Client.Patch(ctx,
		existing,
		client.RawPatch(k8stypes.MergePatchType, []byte(patch)),
	); err != nil {
		if apierrors.IsForbidden(err) {
			return fmt.Errorf("missing required permissions to patch subscription %s/%s", sub.Namespace, sub.Name)
		}

		return fmt.Errorf("patch failed: %w", err)
	}

	return nil
}

// fetchOperatorByPackage fetches the CSV for an operator by package name.
func fetchOperatorByPackage(ctx context.Context, c *openshift.OpenshiftClient, packageName string, opNS string) (*operatorsv1alpha1.ClusterServiceVersion, error) {
	// List all subscriptions in the namespace
	subList := &operatorsv1alpha1.SubscriptionList{}
	if err := c.Client.List(ctx, subList, client.InNamespace(opNS)); err != nil {
		if apierrors.IsForbidden(err) {
			return nil, fmt.Errorf("missing required permissions to list subscriptions")
		}

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

// waitForOperator waits for an operator to be ready after installation.
func waitForOperator(ctx context.Context, c *openshift.OpenshiftClient, packageName string, opNS string) error {
	return wait.PollUntilContextTimeout(ctx, constants.OperatorPollInterval, constants.OperatorPollTimeout, true, func(pollCtx context.Context) (bool, error) {
		csv, err := fetchOperatorByPackage(pollCtx, c, packageName, opNS)
		if err != nil {
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
