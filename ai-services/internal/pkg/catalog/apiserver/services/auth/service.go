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
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
)

const (
	hashNumPartitions = 3 // iterations.salt.hash
)

type Service interface {
	Login(ctx context.Context, username, password string) (accessToken, refreshToken string, err error)
	Logout(ctx context.Context, accessToken string) error
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

// Logout invalidates the provided access token by adding it to the blacklist until its natural expiry time.
// This ensures that even if the token is still valid, it cannot be used for authentication after logout.
func (s *service) Logout(ctx context.Context, accessToken string) error {
	// Parse to get expiry for blacklist TTL
	_, exp, err := s.tokens.ValidateAccessToken(accessToken)
	if err != nil {
		// If token is already invalid, treat as success (idempotent)
		return nil
	}
	s.blacklist.Add(accessToken, exp)

	return nil
}

// RefreshTokens validates the provided refresh token and, if valid, generates and returns a new access token
// and refresh token pair. It also optionally blacklists the old refresh token to prevent reuse (not implemented
// here but can be added with a separate blacklist store for refresh tokens).
func (s *service) RefreshTokens(ctx context.Context, refreshToken string) (string, string, error) {
	uid, exp, err := s.tokens.ValidateRefreshToken(refreshToken)
	if err != nil {
		return "", "", err
	}
	// Optional: rotate refresh by blacklisting the old refresh token (if you also protect refresh endpoints with blacklist)
	_ = exp // here not blacklisting refresh; can be added with separate store.

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
