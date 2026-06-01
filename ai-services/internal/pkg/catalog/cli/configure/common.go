package configure

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	catalogPodman "github.com/project-ai-services/ai-services/internal/pkg/catalog/cli/configure/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"golang.org/x/crypto/pbkdf2"
)

const (
	defaultPasswordIterations = 100000
)

// ConfigureOptions contains the configuration for configuring the catalog service.
type ConfigureOptions struct {
	AdminPassword string
	Runtime       types.RuntimeType
	BaseDir       string
	ArgParams     map[string]string
	HttpsPort     int
}

// Run executes the configure process for the catalog service.
func Run(opts ConfigureOptions) error {
	ctx := context.Background()

	// Generate password hash using PBKDF2
	passwordHash, err := hashPasswordPBKDF2(opts.AdminPassword, defaultPasswordIterations)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Deploy catalog service based on runtime
	switch opts.Runtime {
	case types.RuntimeTypePodman:
		// Determine Podman URI
		podmanURI := getPodmanURI()

		// Determine auth file path
		authFilePath := getAuthFilePath()

		return catalogPodman.DeployCatalog(ctx, podmanURI, authFilePath, passwordHash, opts.BaseDir, opts.ArgParams, opts.HttpsPort)

	case types.RuntimeTypeOpenShift:
		return fmt.Errorf("openshift runtime is not yet supported for catalog configure")

	default:
		return fmt.Errorf("unsupported runtime type: %s", opts.Runtime)
	}
}

// getPodmanURI determines the Podman socket URI.
func getPodmanURI() string {
	// TODO: Need to take care for getting rootless socket
	// Return default local Unix socket
	return "/run/podman/podman.sock"
}

// getAuthFilePath determines the auth.json file path.
func getAuthFilePath() string {
	// TODO: Need to take care for getting rootless user auth file path
	// Return default root user auth file path
	return "/run/user/0/containers/auth.json"
}

// hashPasswordPBKDF2 generates a PBKDF2 hash of the password with a random salt.
func hashPasswordPBKDF2(password string, iteration int) (string, error) {
	salt := make([]byte, constants.Pbkdf2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := pbkdf2.Key([]byte(password), salt, iteration, constants.Pbkdf2KeyLen, sha256.New)

	// Format: iterations.salt.hash (base64 encoded)
	encoded := fmt.Sprintf("%d.%s.%s",
		iteration,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash))

	return encoded, nil
}

// Made with Bob
