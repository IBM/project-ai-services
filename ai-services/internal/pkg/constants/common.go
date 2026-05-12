package constants

import "time"

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
	ErrSecretNotFound    = "no secret with name or id"
)

const (
	DirPerm  = 0755
	FilePerm = 0644
)

// DefaultBaseDir is the single source of truth for the default base directory.
// Change this constant to update the default directory everywhere in the application.
const DefaultBaseDir = "/var/lib/ai-services"

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
