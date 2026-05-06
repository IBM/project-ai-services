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
}

// Run executes the configure process for the catalog service.
func Run(opts ConfigureOptions) error {
	ctx := context.Background()

	// Generate password hash using PBKDF2
	passwordHash, err := hashPasswordPBKDF2(opts.AdminPassword, defaultPasswordIterations)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Convert passwordHash to base64 encoded text for Kubernetes/Podman secret
	passwordHashBase64 := base64.StdEncoding.EncodeToString([]byte(passwordHash))

	// Deploy catalog service based on runtime
	switch opts.Runtime {
	case types.RuntimeTypePodman:
		// Determine Podman URI
		podmanURI := getPodmanURI()

		return catalogPodman.DeployCatalog(ctx, podmanURI, passwordHashBase64, opts.BaseDir, opts.ArgParams)

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
