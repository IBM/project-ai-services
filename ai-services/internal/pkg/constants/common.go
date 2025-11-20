package constants

type ValidationLevel int

const (
	PodStartOn  = "on"
	PodStartOff = "off"
)

const (
	ValidationLevelWarning ValidationLevel = iota
	ValidationLevelError
)
