package vars

import "regexp"

var (
	// SpyreCardAnnotationRegex -> ai-services.io/<containerName>--spyre-cards
	SpyreCardAnnotationRegex = regexp.MustCompile(`^ai-services\.io\/([A-Za-z0-9][-A-Za-z0-9_.]*)--sypre-cards$`)
	// ContainerPortExposeAnnotationRegex -> ai-services.io/<containerName>--<PortName>-expose
	ContainerPortExposeAnnotationRegex = regexp.MustCompile(`^ai-services\.io\/([A-Za-z0-9][-A-Za-z0-9_.]*)--([A-Za-z0-9][-A-Za-z0-9_.]*)-expose$`)
	ToolImage                          = "icr.io/ai-services-cicd/tools:0.2"
	ModelDirectory                     = "/var/lib/ai-services/models"
)

type Label string

var (
	PodPortLabel  Label = "ai-services.io/port/%s"
	TemplateLabel Label = "ai-services.io/template"
	VersionLabel  Label = "ai-services.io/version"
)
