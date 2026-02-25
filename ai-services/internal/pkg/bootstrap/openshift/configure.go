package openshift

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sClient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	pollInterval              = 5 * time.Second
	pollTimeout               = 2 * time.Minute
	externalDeviceReservation = "externalDeviceReservation"
	experimentalMode          = "experimentalMode"
)

func (o *OpenshiftBootstrap) Configure() error {
	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to configure openshift cluster")
	}

	// 1. Apply all yamls
	s := spinner.New("Applying YAMLs")
	s.Start(client.Ctx)

	// iterate through the directory and apply the YAMLs
	if err := applyYamls(client.Ctx, client.Client); err != nil {
		s.Fail("failed to apply YAMLs")

		return fmt.Errorf("error occurred while applying YAMLs: %w", err)
	}
	s.Stop("YAMLs Applied")

	s = spinner.New("Waiting for spyre operator to be ready")
	s.Start(client.Ctx)

	err = waitForSpyreOperator(client.Ctx, client.Client)
	if err != nil {
		s.Stop("spyre operator not ready")

		return fmt.Errorf("spyre operator not ready: %w", err)
	}
	s.Stop("Spyre Operator up and ready")

	/*
		2. Configure Spyre cluster policy
		   2.1 fetch spec from spyre-operator using annotation
		   2.2 remove `externalDeviceReservation` from `experimentalMode`
		   2.3 frame and apply the scp yaml
	*/

	// 2. Configure Spyre cluster policy
	s = spinner.New("Configuring Spyre Cluster Policy")
	s.Start(client.Ctx)

	if err := configureSCP(client, s); err != nil {
		s.Fail("failed to configure spyre cluster policy")

		return fmt.Errorf("error occurred while configuring spyre cluster policy: %w", err)
	}
	s.Stop("Spyre Cluster Policy configured")

	return nil
}

func applyYamls(ctx context.Context, c k8sClient.Client) error {
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{
		FS:      &assets.BootstrapFS,
		Root:    "bootstrap",
		Runtime: types.RuntimeTypeOpenShift,
	})

	yamls, err := tp.LoadYamls()
	if err != nil {
		return fmt.Errorf("error loading yamls: %w", err)
	}

	for _, yaml := range yamls {
		if err := utils.ApplyYaml(ctx, yaml, c); err != nil {
			return fmt.Errorf("failed to apply YAML %s: %w", string(yaml), err)
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
	csv, err := fetchSpyreOperator(client.Ctx, client.Client)
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

func fetchSpyreOperator(ctx context.Context, c k8sClient.Client) (*operatorsv1alpha1.ClusterServiceVersion, error) {
	sub := &operatorsv1alpha1.Subscription{}
	if err := c.Get(ctx, k8sClient.ObjectKey{
		Name:      "spyre-operator",
		Namespace: constants.SpyreOperatorNamespace,
	}, sub); err != nil {
		return nil, err
	}

	csv := &operatorsv1alpha1.ClusterServiceVersion{}
	if err := c.Get(ctx, k8sClient.ObjectKey{
		Name:      sub.Status.CurrentCSV,
		Namespace: constants.SpyreOperatorNamespace,
	}, csv); err != nil {
		return nil, err
	}

	return csv, nil
}

func waitForSpyreOperator(ctx context.Context, c k8sClient.Client) error {
	return wait.PollUntilContextTimeout(ctx, pollInterval, pollTimeout, true, func(ctx context.Context) (bool, error) {
		csv, err := fetchSpyreOperator(ctx, c)
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

		return true, nil
	},
	)
}
