package openshift

import (
	"context"
	"fmt"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeOpenshift "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Delete removes an application and its associated resources.
func (o *OpenshiftApplication) Delete(ctx context.Context, opts types.DeleteOptions) error {
	app := opts.Name
	namespace := app

	// Create a new Helm client
	helmClient, err := helm.NewHelm(namespace)
	if err != nil {
		return fmt.Errorf("failed to create helm client: %w", err)
	}

	// Check if the app exists
	isAppExist, err := helmClient.IsReleaseExist(app)
	if err != nil {
		return fmt.Errorf("failed to check if application exists: %w", err)
	}

	if !isAppExist {
		logger.Infof("Application '%s' does not exist in namespace '%s'\n", app, namespace)

		return nil
	}

	if err := o.confirmDeletion(opts); err != nil {
		return err
	}

	logger.Infoln("Proceeding with deletion...")

	const defaultDeleteTimeout = 5 * time.Minute
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultDeleteTimeout
	}

	s := spinner.New("Deleting application '" + app + "'...")

	s.Start(ctx)

	// Perform helm uninstall
	err = helmClient.Uninstall(app, &helm.UninstallOpts{Timeout: timeout})
	if err != nil {
		s.Fail("failed to delete application")

		return fmt.Errorf("failed to perform app deletion: %w", err)
	}

	s.Stop("Application '" + app + "' deleted successfully")

	if !opts.SkipCleanup {
		logger.Infoln("Cleaning up Persistent Volume Claims...")
		if err := o.cleanupPVCs(ctx, namespace); err != nil {
			return fmt.Errorf("failed to cleanup PVCs: %w", err)
		}
	}

	return nil
}

func (o *OpenshiftApplication) confirmDeletion(opts types.DeleteOptions) error {
	if opts.AutoYes {
		return nil
	}

	confirmDelete, err := utils.ConfirmAction("Are you sure you want to delete the application '" + opts.Name + "'?")
	if err != nil {
		return fmt.Errorf("failed to take user input: %w", err)
	}

	if !confirmDelete {
		logger.Infoln("Deletion cancelled")

		return fmt.Errorf("deletion cancelled")
	}

	return nil
}

// cleanupPVCs manually deletes PVCs linked to this helm release.
func (o *OpenshiftApplication) cleanupPVCs(ctx context.Context, namespace string) error {
	oc, ok := o.runtime.(*runtimeOpenshift.OpenshiftClient)
	if !ok {
		return fmt.Errorf("runtime is not an Openshift")
	}

	// List PVCs with the application label
	labelSelector := fmt.Sprintf("ai-services.io/application=%s", namespace)
	pvcs, err := oc.KubeClient.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list PVCs for cleanup: %w", err)
	}

	for _, pvc := range pvcs.Items {
		err := oc.KubeClient.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, pvc.Name, metav1.DeleteOptions{})
		if err != nil {
			logger.Warningf("Failed to delete PVC '%s': %v\n", pvc.Name, err)

			continue
		}

		logger.Infof("Deleted PVC '%s'\n", pvc.Name)
	}

	return nil
}
