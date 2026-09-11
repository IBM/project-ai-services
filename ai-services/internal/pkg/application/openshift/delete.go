package openshift

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"helm.sh/helm/v4/pkg/storage/driver"
)

// Delete removes an application and its associated resources.
func (o *OpenshiftApplication) Delete(ctx context.Context, opts types.DeleteOptions) error {
	app := opts.Name
	namespace := app

	if err := o.confirmDeletion(opts); err != nil {
		return err
	}

	logger.Infoln("Proceeding with deletion...")

	const defaultDeleteTimeout = 5 * time.Minute
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultDeleteTimeout
	}

	helmClient, err := helm.NewHelm(namespace)
	if err != nil {
		return fmt.Errorf("failed to create helm client: %w", err)
	}

	s := spinner.New("Deleting application '" + app + "'...")

	s.Start(ctx)

	err = helmClient.Uninstall(app, &helm.UninstallOpts{Timeout: timeout})
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			s.Stop("Application '" + app + "' does not exist — nothing to delete")

			return nil
		}

		s.Fail("failed to delete application")

		return fmt.Errorf("failed to perform app deletion: %w", err)
	}

	s.Stop("Application '" + app + "' deleted successfully")

	if !opts.SkipCleanup {
		logger.Debugln("Cleaning up Persistent Volume Claims...")

		if err := o.runtime.DeletePVCs(ctx, fmt.Sprintf("ai-services.io/application=%s", app)); err != nil {
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

		return nil
	}

	return nil
}
