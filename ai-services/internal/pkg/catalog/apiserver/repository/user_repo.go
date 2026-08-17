// Package repository provides interfaces and implementations for data storage and retrieval
// related to users and token blacklisting in the AI Services API server.
package repository

import (
	"context"
	"errors"
	"sync"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
)

var ErrUserNotFound = errors.New("user not found")

type UserRepository interface {
	GetByUserName(ctx context.Context, username string) (*models.User, error)
	GetByID(ctx context.Context, id string) (*models.User, error)
}

type InMemoryUserRepo struct {
	mu         sync.RWMutex
	users      map[string]*models.User
	byUserName map[string]*models.User
}

// NewInMemoryUserRepo returns an empty repository with no seeded users.
func NewInMemoryUserRepo() *InMemoryUserRepo {
	return &InMemoryUserRepo{
		users:      make(map[string]*models.User),
		byUserName: make(map[string]*models.User),
	}
}

// NewInMemoryUserRepoWithAdminHash creates a repo and seeds a single admin user
// with a precomputed hash.
func NewInMemoryUserRepoWithAdminHash(id, username, name, passwordHash string) *InMemoryUserRepo {
	r := NewInMemoryUserRepo()
	r.Upsert(&models.User{
		ID:           id,
		UserName:     username,
		PasswordHash: passwordHash,
		Name:         name,
	})

	return r
}

// Upsert inserts or replaces a user entry. Safe for concurrent use.
// Used both for seeding the admin user and for populating entries after a
// ManageIQ token exchange (Flow B / IBM Power Mission Control passthrough).
func (r *InMemoryUserRepo) Upsert(u *models.User) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.users[u.ID] = u
	r.byUserName[u.UserName] = u
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
