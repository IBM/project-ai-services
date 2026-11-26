package k8s

import "testing"

func TestServiceDNS(t *testing.T) {
	if got, want := ServiceDNS("catalog-api", "ai-services-dev"), "catalog-api.ai-services-dev.svc.cluster.local"; got != want {
		t.Errorf("ServiceDNS() = %q, want %q", got, want)
	}
}
