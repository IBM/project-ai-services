package constants

const (
	PodStartOn       = "on"
	PodStartOff      = "off"
	ApplicationsPath = "/var/lib/ai-services/applications"
)

type ValidationLevel int

const (
	ValidationLevelWarning ValidationLevel = iota
	ValidationLevelError
)

// Container Health status checks
type HealthStatus string

const (
	Ready    HealthStatus = "healthy"
	Starting HealthStatus = "starting"
	NotReady HealthStatus = "unhealthy"
)
