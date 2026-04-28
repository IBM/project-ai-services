package vars

import (
	"regexp"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

var (
	// RuntimeFactory defines Global runtime factory.
	RuntimeFactory *runtime.RuntimeFactory
)

var (
	// SpyreCardAnnotationRegex -> ai-services.io/<containerName>--spyre-cards.
	SpyreCardAnnotationRegex = regexp.MustCompile(`^ai-services\.io\/([A-Za-z0-9][-A-Za-z0-9_.]*)--spyre-cards$`)
	ToolImage                = "icr.io/ai-services/tools:0.7"
	// ModelDirectory is the directory for AI models, uses GetModelsPath() for dynamic resolution.
	ModelDirectory = constants.GetModelsPath()
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
