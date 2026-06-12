package configure

import (
	"context"
	"fmt"

	catalogPodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

// ConfigureOptions contains the configuration for configuring the catalog service.
type ConfigureOptions struct {
	BaseDir       string
	// SSL/TLS certificate configuration
	DomainName  string // Custom domain name for self-signed certificates
	SSLCertPath string // Path to user-provided SSL certificate
	SSLKeyPath  string // Path to user-provided SSL private key
	HttpsPort   int
}

// Run executes the configure process for the catalog service.
func Run(opts ConfigureOptions) error {
	ctx := context.Background()

	// Deploy catalog service based on runtime
	rt := vars.RuntimeFactory.GetRuntimeType()
	switch rt {
	case types.RuntimeTypePodman:
		return catalogPodman.DeployCatalog(ctx, opts.BaseDir, opts.DomainName, opts.SSLCertPath, opts.SSLKeyPath, opts.HttpsPort)

	case types.RuntimeTypeOpenShift:
		return fmt.Errorf("openshift runtime is not yet supported for catalog configure")

	default:
		return fmt.Errorf("unsupported runtime type: %s", rt)
	}
}

// Made with Bob
