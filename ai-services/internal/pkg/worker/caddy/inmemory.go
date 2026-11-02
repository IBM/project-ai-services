package caddy

import (
	"context"
	"fmt"
	"sync"

	"github.com/project-ai-services/ai-services/internal/pkg/proxy"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// NewInMemoryProxyRouter builds a ProxyRouter backed by an in-memory route
// table instead of a Caddy admin API. It is used by the mock runtime
// (AI_SERVICES_MOCK_RUNTIME=true) so a worker can run without Caddy.
func NewInMemoryProxyRouter() *ProxyRouter {
	return &ProxyRouter{pm: &inMemoryProxyManager{routes: map[string]proxy.Route{}}}
}

// inMemoryProxyManager is a proxy.ProxyManager that only records routes.
type inMemoryProxyManager struct {
	mu     sync.Mutex
	routes map[string]proxy.Route
}

// RegisterRoute records the route and returns the URL Caddy would have served
// it on, built from CADDY_HTTPS_PORT the same way the real manager does.
func (m *inMemoryProxyManager) RegisterRoute(_ context.Context, route proxy.Route) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.routes[route.ID] = route

	port := utils.GetEnv(proxy.CaddyHTTPSPortEnvVar, proxy.DefaultHTTPSPort)
	if port == proxy.DefaultHTTPSPort {
		return fmt.Sprintf("https://%s", route.Domain), nil
	}

	return fmt.Sprintf("https://%s:%s", route.Domain, port), nil
}

func (m *inMemoryProxyManager) UnregisterRoute(_ context.Context, routeID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.routes, routeID)

	return nil
}

func (m *inMemoryProxyManager) HealthCheck(_ context.Context) error { return nil }

func (m *inMemoryProxyManager) GetRouteByID(_ context.Context, routeID string) (*proxy.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.routes[routeID]
	if !ok {
		return nil, fmt.Errorf("route %q not found", routeID)
	}

	return &r, nil
}
