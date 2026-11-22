package auth

import (
	"context"
	"sort"
)

// Identity providers recorded on models.User.
const (
	IDPLocal = "local"
	IDPMIQ   = "miq"
	// IDPInternal is the `idp` claim of tokens the catalog issues to itself. No user has it.
	IDPInternal = "internal"
)

// Role vocabulary (design decision D2). Phase 1 only ever issues RoleAdmin.
const (
	RoleAdmin = "admin"
	RoleWrite = "write"
	RoleRead  = "read"
)

// Principal is everything a ScopeResolver may use. Phase 1 fills ID and IDP.
type Principal struct {
	ID   string // catalog user ID, never the user name
	IDP  string // IDPLocal | IDPMIQ
	Type string // "user" (service_account in a later phase)
}

// ScopeResolver decides which roles a principal holds on a service.
// Returning no roles means "no access" and becomes 403. This is the only place
// that decides who gets what; the endpoint and the token format have no admin special case.
type ScopeResolver interface {
	Resolve(ctx context.Context, p Principal, service string) ([]string, error)
}

// ServiceDef describes a service that tokens can be issued for.
type ServiceDef struct {
	// Name is the token audience and the scope prefix.
	Name string
	// CatalogID is the catalog template ID used to find the service's deployed UI endpoints.
	CatalogID string
	// Roles is the set of roles defined for the service.
	Roles []string
}

// ServiceRegistry is the fixed table of services that may be named in a token request.
// An unknown name cannot mint a token for an arbitrary audience.
type ServiceRegistry struct {
	byName map[string]ServiceDef
}

// NewServiceRegistry builds a registry from definitions.
func NewServiceRegistry(defs ...ServiceDef) *ServiceRegistry {
	r := &ServiceRegistry{byName: make(map[string]ServiceDef, len(defs))}
	for _, d := range defs {
		r.byName[d.Name] = d
	}

	return r
}

// DefaultServiceRegistry returns the services that tokens can be issued for: digitize and
// the Digital Assistant services chat, similarity and summarize (DA4: the audience is the
// catalog template ID, so the chat service is "chat", not "chatbot").
func DefaultServiceRegistry() *ServiceRegistry {
	roles := []string{RoleAdmin, RoleWrite, RoleRead}

	return NewServiceRegistry(
		ServiceDef{Name: "digitize", CatalogID: "digitize", Roles: roles},
		ServiceDef{Name: "chat", CatalogID: "chat", Roles: roles},
		ServiceDef{Name: "similarity", CatalogID: "similarity", Roles: roles},
		ServiceDef{Name: "summarize", CatalogID: "summarize", Roles: roles},
	)
}

// Lookup returns the definition of a service.
func (r *ServiceRegistry) Lookup(name string) (ServiceDef, bool) {
	d, ok := r.byName[name]

	return d, ok
}

// Names returns the registered service names, sorted.
func (r *ServiceRegistry) Names() []string {
	names := make([]string, 0, len(r.byName))
	for n := range r.byName {
		names = append(names, n)
	}
	sort.Strings(names)

	return names
}

// BootstrapAdminResolver grants RoleAdmin on every registered service to exactly one
// principal: the local bootstrap admin, identified by user ID and identity provider
// (never by user name). Everyone else gets no roles.
type BootstrapAdminResolver struct {
	adminID  string
	registry *ServiceRegistry
}

// NewBootstrapAdminResolver creates the Phase 1 resolver.
func NewBootstrapAdminResolver(adminID string, registry *ServiceRegistry) *BootstrapAdminResolver {
	return &BootstrapAdminResolver{adminID: adminID, registry: registry}
}

// Resolve implements ScopeResolver.
func (r *BootstrapAdminResolver) Resolve(_ context.Context, p Principal, service string) ([]string, error) {
	if _, ok := r.registry.Lookup(service); !ok {
		return nil, nil
	}
	if p.ID == "" || p.ID != r.adminID || p.IDP != IDPLocal {
		return nil, nil
	}

	return []string{RoleAdmin}, nil
}
