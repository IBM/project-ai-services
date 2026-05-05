package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/pbkdf2"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	catalogconstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
)

const (
	hashNumPartitions = 3 // iterations.salt.hash
)

type Service interface {
	Login(ctx context.Context, username, password string) (accessToken, refreshToken string, err error)
	Logout(ctx context.Context, accessToken, refreshToken string) error
	RefreshTokens(ctx context.Context, refreshToken string) (newAccess, newRefresh string, err error)
	GetUser(ctx context.Context, id string) (*models.User, error)
}

type service struct {
	users     repository.UserRepository
	tokens    *TokenManager
	blacklist repository.TokenBlacklist
}

func NewAuthService(users repository.UserRepository, tokens *TokenManager, blacklist repository.TokenBlacklist) Service {
	return &service{users: users, tokens: tokens, blacklist: blacklist}
}

var ErrInvalidCredentials = errors.New("invalid credentials")

func (s *service) Login(ctx context.Context, username, password string) (string, string, error) {
	u, err := s.users.GetByUserName(ctx, username)
	if err != nil {
		return "", "", ErrInvalidCredentials
	}
	if !verifyPassword(password, u.PasswordHash) {
		return "", "", ErrInvalidCredentials
	}
	access, _, err := s.tokens.GenerateAccessToken(u.ID)
	if err != nil {
		return "", "", err
	}
	refresh, _, err := s.tokens.GenerateRefreshToken(u.ID)
	if err != nil {
		return "", "", err
	}

	return access, refresh, nil
}

// Logout invalidates both the refresh token and access token by adding them to the blacklist until their
// natural expiry time. This operation is idempotent - it always succeeds and only blacklists tokens if they
// are valid. Invalid tokens are ignored, making logout safe to call multiple times.
func (s *service) Logout(ctx context.Context, accessToken, refreshToken string) error {
	// If refresh token exists, try to validate and blacklist it
	if refreshToken != "" {
		_, refreshExp, err := s.tokens.ValidateRefreshToken(refreshToken)
		if err == nil {
			s.blacklist.Add(refreshToken, catalogconstants.TokenTypeRefresh, refreshExp)
		}
		// Ignore invalid tokens
	}

	// If access token exists, try to validate and blacklist it
	if accessToken != "" {
		_, accessExp, err := s.tokens.ValidateAccessToken(accessToken)
		if err == nil {
			s.blacklist.Add(accessToken, catalogconstants.TokenTypeAccess, accessExp)
		}
		// Ignore invalid tokens
	}

	return nil
}

// RefreshTokens validates the provided refresh token and, if valid, generates and returns a new access token
// and refresh token pair. It also blacklists the old refresh token to prevent reuse.
func (s *service) RefreshTokens(ctx context.Context, refreshToken string) (string, string, error) {
	uid, exp, err := s.tokens.ValidateRefreshToken(refreshToken)
	if err != nil {
		return "", "", err
	}

	// Blacklist the old refresh token to prevent reuse
	s.blacklist.Add(refreshToken, catalogconstants.TokenTypeRefresh, exp)

	access, _, err := s.tokens.GenerateAccessToken(uid)
	if err != nil {
		return "", "", err
	}
	newRefresh, _, err := s.tokens.GenerateRefreshToken(uid)
	if err != nil {
		return "", "", err
	}

	return access, newRefresh, nil
}

// GetUser retrieves a user by their unique ID. This can be used in various contexts, such as fetching user details.
func (s *service) GetUser(ctx context.Context, id string) (*models.User, error) {
	return s.users.GetByID(ctx, id)
}

// GenerateRandomSecretKey generates a random secret key of the specified length for signing JWT tokens.
func GenerateRandomSecretKey(length int) ([]byte, error) {
	key := make([]byte, length)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to read random bytes: %w", err)
	}

	return key, nil
}

// verifyPassword verifies a password against a PBKDF2 hash.
func verifyPassword(password, encodedHash string) bool {
	parts := strings.Split(encodedHash, ".")
	if len(parts) != hashNumPartitions {
		return false
	}

	iterations, _ := strconv.Atoi(parts[0])
	salt, _ := base64.RawStdEncoding.DecodeString(parts[1])
	hash, _ := base64.RawStdEncoding.DecodeString(parts[2])

	testHash := pbkdf2.Key([]byte(password), salt, iterations, constants.Pbkdf2KeyLen, sha256.New)

	return subtle.ConstantTimeCompare(hash, testHash) == 1
}
