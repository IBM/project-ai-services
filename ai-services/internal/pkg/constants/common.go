package constants

type ValidationLevel int

const (
	PodStartOn       = "on"
	PodStartOff      = "off"
	ApplicationsPath = "/var/lib/ai-services/applications"
)

const (
	ValidationLevelWarning ValidationLevel = iota
	ValidationLevelError
)
