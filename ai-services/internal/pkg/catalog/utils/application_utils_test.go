package utils

import (
	"testing"

	"github.com/google/uuid"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/catalogns"
)

func setNamespace(t *testing.T, ns string) {
	t.Helper()
	if err := catalogns.Set(ns); err != nil {
		t.Fatalf("set namespace: %v", err)
	}
	t.Cleanup(func() { _ = catalogns.Set(catalogns.Default) })
}

func TestAppNamespace(t *testing.T) {
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")

	tests := []struct {
		name string
		ns   string
		want string
	}{
		{"default namespace", catalogns.Default, "ai-services-12345678"},
		{"non-default namespace", "ai-services-dev", "ai-services-dev-12345678"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setNamespace(t, tt.ns)
			if got := AppNamespace(id); got != tt.want {
				t.Errorf("AppNamespace() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOpenShiftCatalogServiceEndpoint(t *testing.T) {
	tests := []struct {
		name string
		ns   string
		want string
	}{
		{"default namespace", catalogns.Default, "catalog-api.ai-services.svc.cluster.local"},
		{"non-default namespace", "ai-services-dev", "catalog-api.ai-services-dev.svc.cluster.local"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setNamespace(t, tt.ns)
			if got := OpenShiftCatalogServiceEndpoint(); got != tt.want {
				t.Errorf("OpenShiftCatalogServiceEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}
