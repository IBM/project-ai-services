package servicereport

import (
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"k8s.io/klog/v2"
)

type ServiceReportRule struct{}

func NewServiceReportRule() *ServiceReportRule {
	return &ServiceReportRule{}
}

func (r *ServiceReportRule) Name() string {
	return "servicereport"
}

func (r *ServiceReportRule) Verify() error {
	klog.V(2).Infoln("Validating if ServiceReport tool has run on LPAR")
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
	return "ServiceReport tool needs to be run on LPAR, please `ai_services bootstrap configure`"
}
