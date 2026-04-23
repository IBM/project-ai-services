package openshift

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
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
)

const (
	externalDeviceReservation = "externalDeviceReservation"
	experimentalMode          = "experimentalMode"
	operatorFolder            = "02-operators"
	operandFolder             = "03-operands"
	machineConfigFolder       = "01-machine-config"
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

	if err := applyYamlsFromFolder(client, machineConfigFolder); err != nil {
		s.Fail("failed to apply the configurations")

		return fmt.Errorf("error occurred while applying the configurations: %w", err)
	}
	s.Stop("Configurations applied successfully")

	// 2. Apply operators (namespaces, operatorgroups, subscriptions)
	if err := applyYamlsFromFolder(client, operatorFolder); err != nil {
		return fmt.Errorf("error occurred while applying operator configurations: %w", err)
	}

	// 3. Apply operands (CRs) - Does SpyreClusterPolicy configure + applying operand yamls
	s = spinner.New("Applying operand configurations")
	s.Start(client.Ctx)

	if err := configureSCP(client, s); err != nil {
		s.Fail("failed to configure spyre cluster policy")

		return fmt.Errorf("error occurred while configuring spyre cluster policy: %w", err)
	}

	if err := applyYamlsFromFolder(client, operandFolder); err != nil {
		s.Fail("failed to apply operand configurations")

		return fmt.Errorf("error occurred while applying operand configurations: %w", err)
	}
	s.Stop("Operand configurations applied successfully")

	// 4. Wait for all CRs to be ready
	if err := waitForAllCRs(client); err != nil {
		return err
	}

	logger.Infoln("Cluster configured successfully")

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
	s.Stop("SpyreClusterPolicy is ready")

	// Wait for DSCInitialization
	s = spinner.New("Waiting for DSCInitialization to be ready")
	s.Start(client.Ctx)

	err = waitForRHODSResource(client, "DSCInitialization")
	if err != nil {
		s.Fail("DSCInitialization not ready")

		return fmt.Errorf("DSCInitialization not ready: %w", err)
	}
	s.Stop("DSCInitialization is ready")

	// Wait for DataScienceCluster
	s = spinner.New("Waiting for DataScienceCluster to be ready")
	s.Start(client.Ctx)

	err = waitForRHODSResource(client, "DataScienceCluster")
	if err != nil {
		s.Fail("DataScienceCluster not ready")

		return fmt.Errorf("DataScienceCluster not ready: %w", err)
	}
	s.Stop("DataScienceCluster is ready")

	return nil
}

func applyYamlsFromFolder(client *openshift.OpenshiftClient, folder string) error {
	tp := templates.NewEmbedTemplateProvider(&assets.BootstrapFS)

	yamls, err := tp.LoadYamls(folder)
	if err != nil {
		return fmt.Errorf("error loading yamls from %s: %w", folder, err)
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

	csv, err := fetchOperatorByPackage(client, spyreOp.Name, spyreOp.Namespace)
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
		if apierrors.IsForbidden(err) {
			return fmt.Errorf("missing required permissions to create SpyreClusterPolicy")
		}
	}

	return err
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
			if apierrors.IsForbidden(err) {
				return false, fmt.Errorf("missing required permissions to get SpyreClusterPolicy")
			}
			// Handle rate limiting and other transient errors as retryable
			if utils.IsTransientK8sError(err) {
				logger.Infof("Transient error getting SpyreClusterPolicy (rate limit or timeout), retrying...", logger.VerbosityLevelDebug)

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

func waitForRHODSResource(client *openshift.OpenshiftClient, kind string) error {
	return wait.PollUntilContextTimeout(client.Ctx, constants.OperatorPollInterval, constants.OperatorPollTimeout, true, func(ctx context.Context) (bool, error) {
		// Get the existing resource from the cluster
		gvk := schema.GroupVersionKind{
			Group:   strings.ToLower(kind) + ".opendatahub.io",
			Version: constants.VersionV2,
			Kind:    kind,
		}
		resource, exists, err := utils.GetExistingCustomResource(client, gvk)
		if err != nil {
			// Handle rate limiting and other transient errors as retryable
			if utils.IsTransientK8sError(err) {
				logger.Infof("Transient error getting %s (rate limit or timeout), retrying...", kind, logger.VerbosityLevelDebug)

				return false, nil
			}

			return false, fmt.Errorf("failed to get %s: %w", kind, err)
		}

		if !exists {
			logger.Infof("%s not found yet, waiting...", kind, logger.VerbosityLevelDebug)

			return false, nil
		}

		// Check the status.phase of the resource
		phase, found, err := unstructured.NestedString(resource.Object, "status", "phase")
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
