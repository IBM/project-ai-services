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
	// Common flags
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

// Made with Bob
