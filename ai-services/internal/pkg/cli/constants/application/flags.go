package application

// CreateFlags contains all flag names for the 'application create' command.
type CreateFlags struct {
	// Common flags - valid for all runtimes
	SkipValidation string
	Template       string
	Params         string
	Values         string

	// Podman-specific flags
	SkipImageDownload string
	SkipModelDownload string
	ImagePullPolicy   string

	// OpenShift-specific flags
	Timeout string
}

// Create holds the flag constants for the 'application create' command.
var Create = CreateFlags{
	// Common flags - valid for all runtimes
	SkipValidation: "skip-validation",
	Template:       "template",
	Params:         "params",
	Values:         "values",

	// Podman-specific flags
	SkipImageDownload: "skip-image-download",
	SkipModelDownload: "skip-model-download",
	ImagePullPolicy:   "image-pull-policy",

	// OpenShift-specific flags
	Timeout: "timeout",
}

// DeleteFlags contains all flag names for the 'application delete' command.
type DeleteFlags struct {
	// Common flags - valid for all runtimes
	SkipCleanup string
	AutoYes     string

	// OpenShift-specific flags
	Timeout string
}

// Delete holds the flag constants for the 'application delete' command.
var Delete = DeleteFlags{
	// Common flags
	SkipCleanup: "skip-cleanup",
	AutoYes:     "yes",

	// OpenShift-specific flags
	Timeout: "timeout",
}

// LogsFlags contains all flag names for the 'application logs' command.
type LogsFlags struct {
	// Common flags - valid for all runtimes
	Pod       string
	Container string
}

// Logs holds the flag constants for the 'application logs' command.
var Logs = LogsFlags{
	Pod:       "pod",
	Container: "container",
}

// PsFlags contains all flag names for the 'application ps' command.
type PsFlags struct {
	// Common flags - valid for all runtimes
	Output string
}

// Ps holds the flag constants for the 'application ps' command.
var Ps = PsFlags{
	Output: "output",
}

// Made with Bob
