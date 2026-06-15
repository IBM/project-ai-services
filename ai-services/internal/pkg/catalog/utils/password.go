package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"golang.org/x/crypto/pbkdf2"
)

// HashPasswordPBKDF2 generates a PBKDF2 hash of the password with a random salt.
// The hash is returned in the format: iterations.salt.hash (base64 encoded).
func HashPasswordPBKDF2(password string, iteration int) (string, error) {
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
