// Package catalogns holds the OpenShift namespace in which the catalog (backend, DB, UI)
// and worker are deployed. It defaults to the catalog app name and can be overridden
// (dev-only) so that multiple catalogs can coexist on one cluster.
package catalogns

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
)

const (
	// Default is the namespace used unless overridden.
	Default = constants.CatalogAppName

	// MaxLen leaves room for the "-<8 char uuid>" suffix added to per-application
	// namespaces so they stay within the 63 char DNS label limit.
	MaxLen = 54
)

// current is set once while parsing CLI flags, before any reader runs.
var current = Default

// Get returns the catalog namespace.
func Get() string {
	return current
}

// Set overrides the catalog namespace after validating it as a DNS-1123 label.
// The current value is left unchanged when validation fails.
func Set(ns string) error {
	if errs := validation.IsDNS1123Label(ns); len(errs) > 0 {
		return fmt.Errorf("invalid namespace %q: %s", ns, strings.Join(errs, "; "))
	}
	if len(ns) > MaxLen {
		return fmt.Errorf("invalid namespace %q: must be at most %d characters", ns, MaxLen)
	}
	current = ns

	return nil
}
