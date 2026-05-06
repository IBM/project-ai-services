package constants

import (
	"os"
	"path/filepath"
	"strings"
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

// DefaultBaseDir is the single source of truth for the default base directory.
// Change this constant to update the default directory everywhere in the application.
const DefaultBaseDir = "/var/lib/ai-services"

// GetBaseDir returns the base directory from environment variable or default.
// It automatically appends "/ai-services" suffix if not already present.
func GetBaseDir() string {
	baseDir := DefaultBaseDir
	if dir := os.Getenv("AI_SERVICES_BASE_DIR"); dir != "" {
		baseDir = dir
	}

	// Clean the path to remove trailing slashes and normalize
	baseDir = filepath.Clean(baseDir)

	// Ensure the path ends with /ai-services
	if !strings.HasSuffix(baseDir, "/ai-services") {
		baseDir = filepath.Join(baseDir, "ai-services")
	}

	return baseDir
}

// GetApplicationsPath returns the applications path based on the configured base directory.
func GetApplicationsPath() string {
	return filepath.Join(GetBaseDir(), "applications")
}

// GetModelsPath returns the models path based on the configured base directory.
func GetModelsPath() string {
	return filepath.Join(GetBaseDir(), "models")
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
