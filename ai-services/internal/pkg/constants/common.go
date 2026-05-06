package constants

import (
	"os"
	"path/filepath"
	"time"
)

const (
	AIServices           = "ai-services"
	PodStartOn           = "on"
	PodStartOff          = "off"
	OperatorPollInterval = 5 * time.Second
	OperatorPollTimeout  = 3 * time.Minute
	VersionV2            = "v2"
	DSCKind              = "DataScienceCluster"
	DSCIKind             = "DSCInitialization"
	SMTLevel             = 2
)

// DefaultAppDir is the single source of truth for the default application directory.
// Change this constant to update the default directory everywhere in the application.
const DefaultAppDir = "/var/lib/ai-services"

// GetAppDir returns the app directory from environment variable or default.
func GetAppDir() string {
	if dir := os.Getenv("AI_SERVICES_APP_DIR"); dir != "" {
		return dir
	}
	return DefaultAppDir
}

// GetApplicationsPath returns the applications path based on the configured app directory.
func GetApplicationsPath() string {
	return filepath.Join(GetAppDir(), "applications")
}

// GetModelsPath returns the models path based on the configured app directory.
func GetModelsPath() string {
	return filepath.Join(GetAppDir(), "models")
}

// OperatorConfig defines configuration for an operator.
type OperatorConfig struct {
	Name      string
	Package   string
	Namespace string
	Label     string
}

// RequiredOperators defines all operators that need to be installed and ready.
var RequiredOperators = []OperatorConfig{
	{
		Name:      "secondary-scheduler-operator",
		Package:   "openshift-secondary-scheduler-operator",
		Namespace: "openshift-secondary-scheduler-operator",
		Label:     "Secondary Scheduler Operator for Red Hat OpenShift",
	},
	{
		Name:      "openshift-cert-manager-operator",
		Namespace: "cert-manager-operator",
		Label:     "Cert-Manager Operator for Red Hat OpenShift",
	},
	{
		Name:      "servicemeshoperator3",
		Namespace: "openshift-operators",
		Label:     "Red Hat OpenShift Service Mesh 3 Operator",
	},
	{
		Name:      "nfd",
		Namespace: "openshift-nfd",
		Label:     "Node Feature Discovery Operator",
	},
	{
		Name:      "rhods-operator",
		Namespace: "redhat-ods-operator",
		Label:     "Red Hat OpenShift AI Operator",
	},
	{
		Name:      "spyre-operator",
		Namespace: "spyre-operator",
		Label:     "IBM Spyre Operator",
	},
}

type ValidationLevel int

const (
	ValidationLevelWarning ValidationLevel = iota
	ValidationLevelError
	ValidationLevelCritical // Critical failures require immediate exit
)

// HealthStatus represents the type for Container Health status.
type HealthStatus string

const (
	Ready    HealthStatus = "healthy"
	Starting HealthStatus = "starting"
	NotReady HealthStatus = "unhealthy"
)
