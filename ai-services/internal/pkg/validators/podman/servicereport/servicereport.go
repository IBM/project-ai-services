package servicereport

import (
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type ServiceReportRule struct{}

func NewServiceReportRule() *ServiceReportRule {
	return &ServiceReportRule{}
}

func (r *ServiceReportRule) Name() string {
	return "servicereport"
}

func (r *ServiceReportRule) Description() string {
	return "Validates if the ServiceReport tool has been run on the LPAR."
}

func (r *ServiceReportRule) Verify() error {
	logger.Infoln("Validating if ServiceReport tool has run on LPAR", logger.VerbosityLevelDebug)
	if err := helpers.RunServiceReportContainer("servicereport -v -p spyre", "validate"); err != nil {
		return err
	}

	return nil
}

func (r *ServiceReportRule) Message() string {
	return "ServiceReport tool has successfully run on the LPAR"
}

func (r *ServiceReportRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *ServiceReportRule) Hint() string {
	return "ServiceReport tool needs to be run on LPAR, please use `ai-services bootstrap configure`"
}
