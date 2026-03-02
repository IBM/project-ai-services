package constants

import "time"

const (
	PodStartOn             = "on"
	PodStartOff            = "off"
	ApplicationsPath       = "/var/lib/ai-services/applications"
	SpyreOperatorNamespace = "spyre-operator"
	OperatorpPollInterval  = 5 * time.Second
	OperatorpPollTimeout   = 2 * time.Minute
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
