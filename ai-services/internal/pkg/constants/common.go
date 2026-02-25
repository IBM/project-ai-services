package constants

const (
	PodStartOn             = "on"
	PodStartOff            = "off"
	ApplicationsPath       = "/var/lib/ai-services/applications"
	SpyreOperatorNamespace = "spyre-operator"
)

type ValidationLevel int

const (
	ValidationLevelWarning ValidationLevel = iota
	ValidationLevelError
)

// HealthStatus represents the type for Container Health status.
type HealthStatus string

const (
	Ready    HealthStatus = "healthy"
	Starting HealthStatus = "starting"
	NotReady HealthStatus = "unhealthy"
)
