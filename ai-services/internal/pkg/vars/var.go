package vars

import (
	"regexp"
	"time"
)

var (
	// SpyreCardAnnotationRegex -> ai-services.io/<containerName>--spyre-cards.
	SpyreCardAnnotationRegex = regexp.MustCompile(`^ai-services\.io\/([A-Za-z0-9][-A-Za-z0-9_.]*)--spyre-cards$`)
	ToolImage                = "icr.io/ai-services/tools:0.6"
	ModelDirectory           = "/var/lib/ai-services/models"
)

type Label string

var (
	TemplateLabel Label = "ai-services.io/template"
	VersionLabel  Label = "ai-services.io/version"
)

var (
	LparAffinityThreshold = 70
)

var (
	RetryCount    = 3
	RetryInterval = 5 * time.Second
)
