package helpers

import (
	"github.com/google/uuid"
)

const (
	// maxComponentNameLen is the length of the components.name column.
	maxComponentNameLen = 100
	// componentNameSuffixLen is how many characters of the ID are appended to make the name unique.
	componentNameSuffixLen = 8
)

// ComponentName builds the name of a component deployed for an application: the application name
// followed by the first characters of the component's ID, for example "my-app-1a2b3c4d". The
// application name is cut when needed so the whole name fits the components.name column.
//
// A name derived from an ID is unique with overwhelming probability, not by construction: callers
// that need a guarantee still check it against the existing names.
func ComponentName(appName string, id uuid.UUID) string {
	if limit := maxComponentNameLen - componentNameSuffixLen - 1; len(appName) > limit {
		appName = appName[:limit]
	}

	return appName + "-" + id.String()[:componentNameSuffixLen]
}
