package openshift

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	apiyaml "k8s.io/apimachinery/pkg/util/yaml"
	k8sClient "sigs.k8s.io/controller-runtime/pkg/client"
)

type yamlType string

const (
	externalDeviceReservation          = "externalDeviceReservation"
	experimentalMode                   = "experimentalMode"
	operator                  yamlType = "operator"
	operand                   yamlType = "operand"
)

func (o *OpenshiftBootstrap) Configure() error {
	logger.Infoln("Configuring OpenShift cluster")
	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to configure openshift cluster: %w", err)
	}

	// 1. Apply machine-config
	s := spinner.New("Applying the configurations")
	s.Start(client.Ctx)

	if err := applyYamlsFromFolder(client, "01-machine-config", ""); err != nil {
		s.Fail("failed to apply the configurations")

		return fmt.Errorf("error occurred while applying the configurations: %w", err)
	}
	s.Stop("Configurations applied successfully")

	// 2. Apply operators (namespaces, operatorgroups, subscriptions)
	s = spinner.New("Applying operator configurations")
	s.Start(client.Ctx)

	if err := applyYamlsFromFolder(client, "02-operators", operator); err != nil {
		s.Fail("failed to apply operator configurations")

		return fmt.Errorf("error occurred while applying operator configurations: %w", err)
	}
	s.Stop("Operator configurations applied successfully")

	// 3. Wait for all operators to be ready
	if err := waitForAllOperators(client); err != nil {
		return err
	}

	// 4. Apply operands (CRs) - Does SpyreClusterPolicy configure + applying operand yamls
	s = spinner.New("Applying operand configurations")
	s.Start(client.Ctx)

	if err := configureSCP(client, s); err != nil {
		s.Fail("failed to configure spyre cluster policy")

		return fmt.Errorf("error occurred while configuring spyre cluster policy: %w", err)
	}

	if err := applyYamlsFromFolder(client, "03-operands", operand); err != nil {
		s.Fail("failed to apply operand configurations")

		return fmt.Errorf("error occurred while applying operand configurations: %w", err)
	}
	s.Stop("Operand configurations applied successfully")

	// 5. Wait for all CRs to be ready
	if err := waitForAllCRs(client); err != nil {
		return err
	}

	logger.Infoln("Cluster configured successfully")

	return nil
}

func waitForAllOperators(client *openshift.OpenshiftClient) error {
	for _, op := range constants.RequiredOperators {
		s := spinner.New(fmt.Sprintf("Waiting for %s to be ready", op.Label))
		s.Start(client.Ctx)

		err := waitForOperator(client, op.Name, op.Namespace)
		if err != nil {
			s.Fail(fmt.Sprintf("%s not ready", op.Label))

			return fmt.Errorf("%s not ready: %w", op.Label, err)
		}
		s.Stop(fmt.Sprintf("  %s up and ready", op.Label))
	}

	return nil
}

func waitForAllCRs(client *openshift.OpenshiftClient) error {
	// Wait for SpyreClusterPolicy
	s := spinner.New("Waiting for SpyreClusterPolicy to be ready")
	s.Start(client.Ctx)

	err := waitForSpyreClusterPolicy(client)
	if err != nil {
		s.Fail("SpyreClusterPolicy not ready")

		return fmt.Errorf("SpyreClusterPolicy not ready: %w", err)
	}
	s.Stop("  SpyreClusterPolicy is ready")

	// Wait for DSCInitialization
	s = spinner.New("Waiting for DSCInitialization to be ready")
	s.Start(client.Ctx)

	err = waitForRHODSResource(client, "DSCInitialization", "default-dsci")
	if err != nil {
		s.Fail("DSCInitialization not ready")

		return fmt.Errorf("DSCInitialization not ready: %w", err)
	}
	s.Stop("  DSCInitialization is ready")

	// Wait for DataScienceCluster
	s = spinner.New("Waiting for DataScienceCluster to be ready")
	s.Start(client.Ctx)

	err = waitForRHODSResource(client, "DataScienceCluster", "default-dsc")
	if err != nil {
		s.Fail("DataScienceCluster not ready")

		return fmt.Errorf("DataScienceCluster not ready: %w", err)
	}
	s.Stop("  DataScienceCluster is ready")

	return nil
}

func applyYamlsFromFolder(client *openshift.OpenshiftClient, folder string, ytype yamlType) error {
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{
		FS:      &assets.BootstrapFS,
		Root:    "bootstrap/openshift/" + folder,
		Runtime: types.RuntimeTypeOpenShift,
	})

	yamls, err := tp.LoadYamls()
	if err != nil {
		return fmt.Errorf("error loading yamls from %s: %w", folder, err)
	}

	switch ytype {
	case operand:
		// For operands, check if DSC/DSCI already exist and update existing resources
		yamls, err = handleExistingOperands(client, yamls)
		if err != nil {
			return fmt.Errorf("error handling existing operands: %w", err)
		}
	}

	for _, yaml := range yamls {
		if err := applyYaml(client, yaml); err != nil {
			return fmt.Errorf("failed to apply YAML from %s: %w", folder, err)
		}
	}

	return nil
}

func configureSCP(client *openshift.OpenshiftClient, s *spinner.Spinner) error {
	// fetch spec from spyre operator alm-example
	spec, err := fetchSCPSpec(client)
	if err != nil {
		return fmt.Errorf("error occurred while fetching spyre cluster policy spec: %w", err)
	}

	// remove externalDeviceReservation from experimentalMode underSpec
	if err = modifySpec(spec, s); err != nil {
		return fmt.Errorf("error occurred while modifying spyre cluster policy spec: %w", err)
	}

	// frame and apply the scp yaml
	if err = frameAndApply(client, spec, s); err != nil {
		return fmt.Errorf("error occurred while applying patch to spyre cluster policy: %w", err)
	}

	return nil
}

func fetchSCPSpec(client *openshift.OpenshiftClient) (map[string]any, error) {
	// Find Spyre operator config
	var spyreOp constants.OperatorConfig
	for _, op := range constants.RequiredOperators {
		if op.Name == "spyre-operator" {
			spyreOp = op

			break
		}
	}

	csv, err := fetchOperator(client, spyreOp.Name, spyreOp.Namespace)
	if err != nil {
		return nil, fmt.Errorf("error fetching spyre operator: %w", err)
	}

	almExample, ok := csv.Annotations["alm-examples"]
	if !ok {
		return nil, fmt.Errorf("alm-example annotation not found")
	}

	var examples []map[string]any
	if err := json.Unmarshal([]byte(almExample), &examples); err != nil {
		return nil, fmt.Errorf("error unmarshalling alm-examples: %w", err)
	}

	for _, ex := range examples {
		if ex["kind"] != "SpyreClusterPolicy" {
			continue
		}
		if spec, ok := ex["spec"].(map[string]any); ok {
			return spec, nil
		}
	}

	return nil, fmt.Errorf("SpyreClusterPolicy not found")
}

// modifySpec remove `externalDeviceReservation` from `experimentalMode`.
func modifySpec(spec map[string]any, s *spinner.Spinner) error {
	expMode, ok := spec[experimentalMode].([]any)
	if !ok {
		logger.Infof("%s not found, proceeding with deployment of SpyreClusterPolicy", experimentalMode, logger.VerbosityLevelDebug)

		return nil
	}

	// updatedExpMode holds filtered list after removing `externalDeviceReservation`
	updatedExpMode := make([]any, 0, len(expMode))

	for _, item := range expMode {
		mode, ok := item.(string)
		if !ok {
			// if the type is unexpected, keep it to avoid data loss
			updatedExpMode = append(updatedExpMode, item)

			continue
		}

		if mode == externalDeviceReservation {
			s.UpdateMessage("Found " + externalDeviceReservation + "under " + experimentalMode + ", removing it")

			continue
		}

		updatedExpMode = append(updatedExpMode, mode)
	}
	spec[experimentalMode] = updatedExpMode

	return nil
}

func frameAndApply(client *openshift.OpenshiftClient, spec map[string]any, s *spinner.Spinner) error {
	scp := &unstructured.Unstructured{}
	c := client.Client
	ctx := client.Ctx
	scp.SetName("spyreclusterpolicy")
	scp.Object = map[string]any{
		"apiVersion": "spyre.ibm.com/v1alpha1",
		"kind":       "SpyreClusterPolicy",
		"metadata": map[string]any{
			"name": "spyreclusterpolicy",
		},
		"spec": spec,
	}

	err := c.Create(ctx, scp)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			s.UpdateMessage("SpyreClusterPolicy already exists")

			return nil
		}
	}

	return err
}

func fetchOperator(client *openshift.OpenshiftClient, opName string, opNS string) (*operatorsv1alpha1.ClusterServiceVersion, error) {
	sub := &operatorsv1alpha1.Subscription{}
	if err := client.Client.Get(client.Ctx, k8sClient.ObjectKey{
		Name:      opName,
		Namespace: opNS,
	}, sub); err != nil {
		return nil, err
	}

	// Use installedCSV from status instead of startingCSV from spec
	if sub.Status.InstalledCSV == "" {
		return nil, apierrors.NewNotFound(operatorsv1alpha1.Resource("clusterserviceversion"), "")
	}

	csv := &operatorsv1alpha1.ClusterServiceVersion{}
	if err := client.Client.Get(client.Ctx, k8sClient.ObjectKey{
		Name:      sub.Status.InstalledCSV,
		Namespace: opNS,
	}, csv); err != nil {
		return nil, err
	}

	return csv, nil
}

func waitForOperator(client *openshift.OpenshiftClient, opName string, opNS string) error {
	return wait.PollUntilContextTimeout(client.Ctx, constants.OperatorPollInterval, constants.OperatorPollTimeout, true, func(ctx context.Context) (bool, error) {
		csv, err := fetchOperator(client, opName, opNS)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// keep waiting until timeout
				return false, nil
			}

			return false, err
		}
		// found
		if csv.Status.Phase == operatorsv1alpha1.CSVPhaseSucceeded {
			return true, nil
		}

		return false, nil
	},
	)
}

func waitForSpyreClusterPolicy(client *openshift.OpenshiftClient) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "spyre.ibm.com",
		Version: "v1alpha1",
		Kind:    "SpyreClusterPolicy",
	})

	return wait.PollUntilContextTimeout(client.Ctx, constants.OperatorPollInterval, constants.OperatorPollTimeout, true, func(ctx context.Context) (bool, error) {
		if err := client.Client.Get(ctx, k8stypes.NamespacedName{Name: "spyreclusterpolicy"}, obj); err != nil {
			if apierrors.IsNotFound(err) {
				logger.Infof("SpyreClusterPolicy not found yet, waiting...", logger.VerbosityLevelDebug)

				return false, nil
			}

			return false, fmt.Errorf("failed to get SpyreClusterPolicy: %w", err)
		}

		state, found, err := unstructured.NestedString(obj.Object, "status", "state")
		if err != nil {
			return false, fmt.Errorf("failed to parse status.state: %w", err)
		}

		if !found || state != "ready" {
			if !found {
				state = "unknown"
			}
			logger.Infof("SpyreClusterPolicy not ready yet (status.state: %s), waiting...", state, logger.VerbosityLevelDebug)

			return false, nil
		}

		return true, nil
	})
}

// handleExistingOperands checks if DSC/DSCI instances already exist and updates existing resources.
func handleExistingOperands(client *openshift.OpenshiftClient, yamls [][]byte) ([][]byte, error) {
	updatedYamls := make([][]byte, 0, len(yamls))

	for _, yaml := range yamls {
		updatedYaml, err := updateYamlWithExistingResource(client, yaml)
		if err != nil {
			return nil, err
		}
		updatedYamls = append(updatedYamls, updatedYaml)
	}

	return updatedYamls, nil
}

// updateYamlWithExistingResource checks if DSC/DSCI resources exist and updates existing resources.
// This function handles multi-resource YAML files (resources separated by ---).
func updateYamlWithExistingResource(client *openshift.OpenshiftClient, yaml []byte) ([]byte, error) {
	decoder := apiyaml.NewYAMLOrJSONDecoder(bytes.NewReader(yaml), yamlDecoderBufSz)
	var updatedResources [][]byte

	// Process all resources in the YAML file
	for {
		resource := &unstructured.Unstructured{}
		err := decoder.Decode(resource)

		if err == io.EOF {
			break
		}

		if err != nil {
			return nil, fmt.Errorf("error decoding yaml: %w", err)
		}

		// Skip empty resources
		if resource.GetKind() == "" {
			continue
		}

		// Process DSC/DSCI resources to check for existing instances
		if err := processRHODSResource(client, resource); err != nil {
			return nil, err
		}

		// Convert resource back to JSON (YAML decoder can handle JSON)
		resourceJSON, err := resource.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("error marshaling resource: %w", err)
		}

		updatedResources = append(updatedResources, resourceJSON)
	}

	// If no resources were found, return original YAML
	if len(updatedResources) == 0 {
		return yaml, nil
	}

	return combineResources(updatedResources), nil
}

// processRHODSResource checks if a DSC/DSCI resource exists and updates its name if needed.
func processRHODSResource(client *openshift.OpenshiftClient, resource *unstructured.Unstructured) error {
	kind := resource.GetKind()

	// Only handle DSCInitialization and DataScienceCluster
	if kind != "DSCInitialization" && kind != "DataScienceCluster" {
		return nil
	}

	// Check if an instance of this kind already exists
	existingName, exists, err := getExistingResourceName(client, kind)
	if err != nil {
		return fmt.Errorf("error checking for existing %s: %w", kind, err)
	}

	if exists {
		logger.Infof("\nFound existing %s named '%s', will re-apply spec to it", kind, existingName, logger.VerbosityLevelDebug)
		resource.SetName(existingName)
	} else {
		logger.Infof("\nNo existing %s found, will create new one", kind, logger.VerbosityLevelDebug)
	}

	return nil
}

// combineResources combines multiple JSON resources with YAML separator.
func combineResources(resources [][]byte) []byte {
	var result bytes.Buffer
	for i, res := range resources {
		if i > 0 {
			result.WriteString("\n---\n")
		}
		result.Write(res)
	}

	return result.Bytes()
}

// getExistingResourceName checks if a DSC/DSCI resource exists and returns its name.
func getExistingResourceName(client *openshift.OpenshiftClient, kind string) (string, bool, error) {
	var group, version string

	switch kind {
	case "DSCInitialization":
		group = "dscinitialization.opendatahub.io"
		version = "v1"
	case "DataScienceCluster":
		group = "datasciencecluster.opendatahub.io"
		version = "v1"
	default:
		return "", false, nil
	}

	// List all resources of this kind
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   group,
		Version: version,
		Kind:    kind + "List",
	})

	if err := client.Client.List(client.Ctx, list); err != nil {
		if apierrors.IsNotFound(err) {
			return "", false, nil
		}

		return "", false, fmt.Errorf("error listing %s: %w", kind, err)
	}

	if len(list.Items) == 0 {
		return "", false, nil
	}

	// Return the name of the first instance found
	return list.Items[0].GetName(), true, nil
}

func waitForRHODSResource(client *openshift.OpenshiftClient, kind, name string) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   strings.ToLower(kind) + ".opendatahub.io",
		Version: "v2",
		Kind:    kind,
	})

	return wait.PollUntilContextTimeout(client.Ctx, constants.OperatorPollInterval, constants.OperatorPollTimeout, true, func(ctx context.Context) (bool, error) {
		if err := client.Client.Get(ctx, k8stypes.NamespacedName{Name: name}, obj); err != nil {
			if apierrors.IsNotFound(err) {
				logger.Infof("%s not found yet, waiting...", kind, logger.VerbosityLevelDebug)

				return false, nil
			}

			return false, fmt.Errorf("failed to get %s: %w", kind, err)
		}

		phase, found, err := unstructured.NestedString(obj.Object, "status", "phase")
		if err != nil {
			return false, fmt.Errorf("failed to parse status.phase: %w", err)
		}

		if !found || phase != "Ready" {
			if !found {
				phase = "unknown"
			}
			logger.Infof("%s not ready yet (status.phase: %s), waiting...", kind, phase, logger.VerbosityLevelDebug)

			return false, nil
		}

		return true, nil
	})
}
