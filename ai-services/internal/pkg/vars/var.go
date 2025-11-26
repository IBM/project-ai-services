package vars

import "regexp"

var (
	// SpyreCardAnnotationRegex -> ai-services.io/<containerName>--spyre-cards
	SpyreCardAnnotationRegex = regexp.MustCompile(`^ai-services\.io\/([A-Za-z0-9][-A-Za-z0-9_.]*)--spyre-cards$`)
	ToolImage                = "icr.io/ai-services-cicd/tools:0.3"
	ModelDirectory           = "/var/lib/ai-services/models"
)

type Label string

var (
	TemplateLabel Label = "ai-services.io/template"
	VersionLabel  Label = "ai-services.io/version"
)
