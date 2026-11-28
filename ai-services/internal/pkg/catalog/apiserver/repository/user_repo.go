// Package repository provides interfaces and implementations for data storage and retrieval
// related to users and token blacklisting in the AI Launchpad API server.
package repository

import (
	"context"
	"errors"
	"sync"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
)

var ErrUserNotFound = errors.New("user not found")

// ErrReservedIdentity is returned when an upsert would replace, or collide with, a
// protected (bootstrap) user.
var ErrReservedIdentity = errors.New("identity is reserved")

// idpLocal is the identity provider of locally authenticated users. It is duplicated here
// (and not imported from the auth package) because auth imports this package.
const idpLocal = "local"

type UserRepository interface {
	GetByUserName(ctx context.Context, username string) (*models.User, error)
	GetByID(ctx context.Context, id string) (*models.User, error)
}

type InMemoryUserRepo struct {
	mu         sync.RWMutex
	users      map[string]*models.User
	byUserName map[string]*models.User
	// protectedIDs and protectedNames hold the seeded bootstrap users. Upsert refuses
	// to touch them, so an externally supplied identity (for example ManageIQ) can
	// never replace the admin or take over its ID or user name.
	protectedIDs   map[string]struct{}
	protectedNames map[string]struct{}
}

// NewInMemoryUserRepo returns an empty repository with no seeded users.
func NewInMemoryUserRepo() *InMemoryUserRepo {
	return &InMemoryUserRepo{
		users:          make(map[string]*models.User),
		byUserName:     make(map[string]*models.User),
		protectedIDs:   make(map[string]struct{}),
		protectedNames: make(map[string]struct{}),
	}
}

// NewInMemoryUserRepoWithAdminHash creates a repo and seeds a single admin user
// with a precomputed hash. The admin is a protected, local-IDP user.
func NewInMemoryUserRepoWithAdminHash(id, username, name, passwordHash string) *InMemoryUserRepo {
	r := NewInMemoryUserRepo()
	u := &models.User{
		ID:           id,
		UserName:     username,
		PasswordHash: passwordHash,
		Name:         name,
		IDP:          idpLocal,
	}
	r.users[u.ID] = u
	r.byUserName[u.UserName] = u
	r.protectedIDs[u.ID] = struct{}{}
	r.protectedNames[u.UserName] = struct{}{}

	return r
}

// Upsert inserts or replaces a user entry. Safe for concurrent use.
// Used to populate entries after a ManageIQ token exchange (Flow B / IBM Power
// Mission Control passthrough). It returns ErrReservedIdentity, and changes nothing,
// when the ID or user name belongs to a protected bootstrap user.
func (r *InMemoryUserRepo) Upsert(u *models.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, reserved := r.protectedIDs[u.ID]; reserved {
		return ErrReservedIdentity
	}
	if _, reserved := r.protectedNames[u.UserName]; reserved {
		return ErrReservedIdentity
	}

	r.users[u.ID] = u
	r.byUserName[u.UserName] = u

	return nil
}

// GetByUserName retrieves a user by their username. It returns ErrUserNotFound if no user with the given username exists.
func (r *InMemoryUserRepo) GetByUserName(ctx context.Context, username string) (*models.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.byUserName[username]
	if !ok {
		return nil, ErrUserNotFound
	}

	return u, nil
}

// GetByID retrieves a user by their ID. It returns ErrUserNotFound if no user with the given ID exists.
func (r *InMemoryUserRepo) GetByID(ctx context.Context, id string) (*models.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}

	return u, nil
}
