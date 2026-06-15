package configure

import (
	"context"
	"fmt"

	catalogPodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// Run executes the configure process for the catalog service.
// It creates runtime-specific options and calls the appropriate runtime implementation.
func Run(runtime types.RuntimeType, baseDir, domainName, sslCertPath, sslKeyPath string, httpsPort int) error {
	ctx := context.Background()
	// Deploy catalog service based on runtime
	switch runtime {
	case types.RuntimeTypePodman:
		opts := catalogPodman.PodmanConfigureOptions{
			BaseDir:     baseDir,
			DomainName:  domainName,
			SSLCertPath: sslCertPath,
			SSLKeyPath:  sslKeyPath,
			HttpsPort:   httpsPort,
		}

		return catalogPodman.DeployCatalog(ctx, opts)

	case types.RuntimeTypeOpenShift:
		return fmt.Errorf("openshift runtime is not yet supported for catalog configure")

	default:
		return fmt.Errorf("unsupported runtime type: %s", runtime)
	}
}

// Made with Bob
