package logger

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/utils/sanitize"
)

// defaultSanitizer is the package-level SecretSanitizer used by all logger
// functions. It is initialised once at startup via NewSecretSanitizer.
var defaultSanitizer = sanitize.NewSecretSanitizer()

// sanitizedSprintf is a drop-in replacement for fmt.Sprintf that sanitises
// all map arguments through the SecretSanitizer before formatting.
// All *fCtx logger functions route through this call.
func sanitizedSprintf(format string, args ...any) string {
	return fmt.Sprintf(format, defaultSanitizer.SanitizeArgs(args)...)
}
