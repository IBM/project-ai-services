package openshift

import (
	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Logs displays logs from an application pod.
func (o *OpenshiftApplication) Logs(opts types.LogsOptions) error {
	logger.Warningln("not implemented")

	return nil
}
