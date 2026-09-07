package constants

import "time"

const (
	AIServices           = "ai-services"
	PodStartOn           = "on"
	PodStartOff          = "off"
	OperatorPollInterval = 5 * time.Second
	OperatorPollTimeout  = 3 * time.Minute

	// HelmTimeout is the default timeout for a Helm install/upgrade operation.
	HelmTimeout = 10 * time.Minute
	// HelmUninstallTimeout is the default timeout for a Helm uninstall operation.
	HelmUninstallTimeout = 5 * time.Minute
	// PredictorWaitTimeout is the maximum time to wait for a KServe InferenceService
	// to reach Ready=True after a Helm install.
	PredictorWaitTimeout = 15 * time.Minute
	// IsvcPollInterval is the interval between KServe InferenceService readiness polls.
	IsvcPollInterval  = 15 * time.Second
	VersionV2         = "v2"
	DSCKind           = "DataScienceCluster"
	DSCIKind          = "DSCInitialization"
	SMTLevel          = 2
	ErrSecretNotFound = "no secret with name or id"
	CaddyServerName   = "ai_services" // Caddy server name used for route registration
)

const (
	DirPerm  = 0755
	FilePerm = 0644
)

// Ulimit configuration constants.
const (
	MinNofileLimit        = 134217728
	NofileFieldCount      = 4
	MemlockConfFile       = "/etc/security/limits.d/memlock.conf"
	NofileConfFile        = "/etc/security/limits.conf"
	MemlockConfContent    = "@sentient - memlock unlimited\n"
	NofileConfTemplate    = "@sentient hard nofile %d\n"
	SentientGroupName     = "sentient"
	ExpectedMemlockConfig = "@sentient - memlock unlimited"
	ExpectedNofileConfig  = "@sentient hard nofile 134217728"
)

const (
	PercentageDivisor = 100.0
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

const (
	// PodmanAuthSecret represents podman auth secret name.
	PodmanAuthSecret = "podman-auth-secret"
	// VolumeLabel represents the volume name associated with Pod.
	VolumeLabel = "ai-services.io/volume"
)
