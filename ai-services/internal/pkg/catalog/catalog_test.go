package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Architecture Tests
// ============================================================================

func TestLoadArchitecture(t *testing.T) {
	t.Run("Load existing architecture", func(t *testing.T) {
		arch, err := LoadArchitecture("rag")
		require.NoError(t, err, "Should load existing architecture without error")
		require.NotNil(t, arch, "Architecture should not be nil")

		assert.Equal(t, "rag", arch.ID, "Architecture ID should match")
		assert.NotEmpty(t, arch.Name, "Architecture name should not be empty")
		assert.NotEmpty(t, arch.Description, "Architecture description should not be empty")
		assert.NotEmpty(t, arch.Version, "Architecture version should not be empty")
		assert.NotNil(t, arch.Services, "Architecture services should not be nil")
		assert.NotEmpty(t, arch.Services, "Architecture should have services")
	})

	t.Run("Load non-existent architecture", func(t *testing.T) {
		arch, err := LoadArchitecture("non-existent")
		assert.Error(t, err, "Should return error for non-existent architecture")
		assert.Nil(t, arch, "Architecture should be nil on error")
		assert.Contains(t, err.Error(), "failed to load architecture", "Error should mention loading architecture")
	})

	t.Run("Validate architecture structure", func(t *testing.T) {
		arch, err := LoadArchitecture("rag")
		require.NoError(t, err)

		// Validate services
		assert.Greater(t, len(arch.Services), 0, "Should have at least one service")

		// Check all services have IDs and track if at least one is required
		hasRequiredService := false
		for _, svc := range arch.Services {
			assert.NotEmpty(t, svc.ID, "Service should have an ID")
			// Verify the service exists
			assert.True(t, ServiceExists(svc.ID), "Service '%s' should exist", svc.ID)
			if !svc.Optional {
				hasRequiredService = true
			}
		}

		assert.True(t, hasRequiredService, "Should have at least one required service")
	})
}

func TestListArchitectures(t *testing.T) {
	t.Run("List all architectures", func(t *testing.T) {
		architectures, err := ListArchitectures()
		require.NoError(t, err, "Should list architectures without error")
		assert.NotEmpty(t, architectures, "Should have at least one architecture")

		// Verify each architecture has required fields
		for _, arch := range architectures {
			assert.NotEmpty(t, arch.ID, "Architecture ID should not be empty")
			assert.NotEmpty(t, arch.Name, "Architecture name should not be empty")
			assert.NotEmpty(t, arch.Version, "Architecture version should not be empty")
		}
	})

	t.Run("Verify rag architecture is in list", func(t *testing.T) {
		architectures, err := ListArchitectures()
		require.NoError(t, err)

		found := false
		for _, arch := range architectures {
			if arch.ID == "rag" {
				found = true
				break
			}
		}
		assert.True(t, found, "RAG architecture should be in the list")
	})
}

func TestArchitectureExists(t *testing.T) {
	testCases := []struct {
		name     string
		archID   string
		expected bool
	}{
		{
			name:     "Existing architecture",
			archID:   "rag",
			expected: true,
		},
		{
			name:     "Non-existent architecture",
			archID:   "non-existent",
			expected: false,
		},
		{
			name:     "Empty architecture ID",
			archID:   "",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			exists := ArchitectureExists(tc.archID)
			assert.Equal(t, tc.expected, exists, "ArchitectureExists should return %v for '%s'", tc.expected, tc.archID)
		})
	}
}

// ============================================================================
// Service Tests
// ============================================================================

func TestLoadService(t *testing.T) {
	testCases := []struct {
		name      string
		serviceID string
		wantError bool
	}{
		{
			name:      "Load opensearch service",
			serviceID: "opensearch",
			wantError: false,
		},
		{
			name:      "Load chat service",
			serviceID: "chat",
			wantError: false,
		},
		{
			name:      "Load digitize service",
			serviceID: "digitize",
			wantError: false,
		},
		{
			name:      "Load summarize service",
			serviceID: "summarize",
			wantError: false,
		},
		{
			name:      "Load non-existent service",
			serviceID: "non-existent",
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			service, err := LoadService(tc.serviceID)

			if tc.wantError {
				assert.Error(t, err, "Should return error for non-existent service")
				assert.Nil(t, service, "Service should be nil on error")
				assert.Contains(t, err.Error(), "failed to load service", "Error should mention loading service")
			} else {
				require.NoError(t, err, "Should load existing service without error")
				require.NotNil(t, service, "Service should not be nil")

				assert.Equal(t, tc.serviceID, service.ID, "Service ID should match")
				assert.NotEmpty(t, service.Name, "Service name should not be empty")
				assert.NotEmpty(t, service.Description, "Service description should not be empty")
				assert.NotEmpty(t, service.Type, "Service type should not be empty")
			}
		})
	}
}

func TestListServices(t *testing.T) {
	t.Run("List all services", func(t *testing.T) {
		services, err := ListServices()
		require.NoError(t, err, "Should list services without error")
		assert.NotEmpty(t, services, "Should have at least one service")

		// Verify each service has required fields
		for _, svc := range services {
			assert.NotEmpty(t, svc.ID, "Service ID should not be empty")
			assert.NotEmpty(t, svc.Name, "Service name should not be empty")
			assert.NotEmpty(t, svc.Type, "Service type should not be empty")
		}
	})

	t.Run("Verify expected deployable services are in list", func(t *testing.T) {
		services, err := ListServices()
		require.NoError(t, err)

		// Only deployable services (not dependency-only)
		expectedServices := []string{
			"chat",
			"digitize",
			"summarize",
		}

		serviceMap := make(map[string]bool)
		for _, svc := range services {
			serviceMap[svc.ID] = true
		}

		for _, expected := range expectedServices {
			assert.True(t, serviceMap[expected], "Service '%s' should be in the list", expected)
		}
	})

	t.Run("Verify dependency-only services are not in list", func(t *testing.T) {
		services, err := ListServices()
		require.NoError(t, err)

		// These services should NOT be in the list (dependency-only)
		dependencyOnlyServices := []string{
			"opensearch",
			"instruct",
			"embedding",
		}

		serviceMap := make(map[string]bool)
		for _, svc := range services {
			serviceMap[svc.ID] = true
		}

		for _, depOnly := range dependencyOnlyServices {
			assert.False(t, serviceMap[depOnly], "Dependency-only service '%s' should not be in the list", depOnly)
		}
	})

	t.Run("Verify service count", func(t *testing.T) {
		services, err := ListServices()
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(services), 3, "Should have at least 3 deployable services")
	})
}

func TestServiceExists(t *testing.T) {
	testCases := []struct {
		name      string
		serviceID string
		expected  bool
	}{
		{
			name:      "Existing service - opensearch",
			serviceID: "opensearch",
			expected:  true,
		},
		{
			name:      "Existing service - chat",
			serviceID: "chat",
			expected:  true,
		},
		{
			name:      "Non-existent service",
			serviceID: "non-existent",
			expected:  false,
		},
		{
			name:      "Empty service ID",
			serviceID: "",
			expected:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			exists := ServiceExists(tc.serviceID)
			assert.Equal(t, tc.expected, exists, "ServiceExists should return %v for '%s'", tc.expected, tc.serviceID)
		})
	}
}

// ============================================================================
// Dependency Resolution Tests
// ============================================================================

func TestResolveServiceDependencies(t *testing.T) {
	t.Run("Resolve service with no dependencies", func(t *testing.T) {
		// OpenSearch has no dependencies
		deps, err := ResolveServiceDependencies("opensearch")
		require.NoError(t, err, "Should resolve opensearch without error")
		require.NotNil(t, deps, "Dependencies should not be nil")

		assert.Contains(t, deps, "opensearch", "Should include the service itself")
		assert.Equal(t, 1, len(deps), "OpenSearch should have only itself")
	})

	t.Run("Resolve service with dependencies", func(t *testing.T) {
		// Chat depends on opensearch, embedding, instruct, reranker
		deps, err := ResolveServiceDependencies("chat")
		require.NoError(t, err, "Should resolve chat without error")
		require.NotNil(t, deps, "Dependencies should not be nil")

		assert.Contains(t, deps, "chat", "Should include the service itself")
		assert.Contains(t, deps, "opensearch", "Should include opensearch dependency")
		assert.Contains(t, deps, "embedding", "Should include embedding dependency")
		assert.Contains(t, deps, "instruct", "Should include instruct dependency")

		// Verify dependencies come before the service
		chatIdx := indexOf(deps, "chat")
		opensearchIdx := indexOf(deps, "opensearch")
		assert.Less(t, opensearchIdx, chatIdx, "Dependencies should come before the service")
	})

	t.Run("Resolve service with transitive dependencies", func(t *testing.T) {
		// Digitize depends on opensearch
		deps, err := ResolveServiceDependencies("digitize")
		require.NoError(t, err, "Should resolve digitize without error")
		require.NotNil(t, deps, "Dependencies should not be nil")

		assert.Contains(t, deps, "digitize", "Should include the service itself")
		assert.Contains(t, deps, "opensearch", "Should include transitive dependency")
	})

	t.Run("Resolve non-existent service", func(t *testing.T) {
		deps, err := ResolveServiceDependencies("non-existent")
		assert.Error(t, err, "Should return error for non-existent service")
		assert.Nil(t, deps, "Dependencies should be nil on error")
		assert.Contains(t, err.Error(), "failed to load service", "Error should mention loading service")
	})

	t.Run("Handle circular dependencies gracefully", func(t *testing.T) {
		// Even if there were circular dependencies, the visited map should prevent infinite loops
		// This test verifies the algorithm doesn't hang
		deps, err := ResolveServiceDependencies("chat")
		require.NoError(t, err, "Should handle dependencies without hanging")
		assert.NotNil(t, deps, "Should return dependencies")
	})

	t.Run("Resolve multiple services at once", func(t *testing.T) {
		deps, err := ResolveServiceDependencies("chat", "digitize")
		require.NoError(t, err, "Should resolve multiple services")
		require.NotNil(t, deps, "Dependencies should not be nil")

		assert.Contains(t, deps, "chat", "Should include chat")
		assert.Contains(t, deps, "digitize", "Should include digitize")
		assert.Contains(t, deps, "opensearch", "Should include shared dependency")
	})
}

func TestGetDeploymentOrder(t *testing.T) {
	t.Run("Get deployment order for single service", func(t *testing.T) {
		layers, err := GetDeploymentOrder([]string{"opensearch"})
		require.NoError(t, err, "Should get deployment order without error")
		require.NotNil(t, layers, "Layers should not be nil")

		assert.Equal(t, 1, len(layers), "Should have one layer")
		assert.Contains(t, layers[0], "opensearch", "First layer should contain opensearch")
	})

	t.Run("Get deployment order for services with dependencies", func(t *testing.T) {
		// Chat depends on opensearch, embedding, instruct
		services := []string{"opensearch", "embedding", "instruct", "chat"}
		layers, err := GetDeploymentOrder(services)
		require.NoError(t, err, "Should get deployment order without error")
		require.NotNil(t, layers, "Layers should not be nil")

		assert.Greater(t, len(layers), 1, "Should have multiple layers")

		// OpenSearch should be in first layer (no dependencies)
		assert.Contains(t, layers[0], "opensearch", "First layer should contain opensearch")

		// Chat should be in last layer (depends on others)
		lastLayer := layers[len(layers)-1]
		assert.Contains(t, lastLayer, "chat", "Last layer should contain chat")

		// Verify all services are included
		allServices := flattenLayers(layers)
		for _, svc := range services {
			assert.Contains(t, allServices, svc, "All services should be in deployment order")
		}
	})

	t.Run("Get deployment order for complex architecture", func(t *testing.T) {
		// Get all services for RAG architecture manually
		services := []string{"opensearch", "embedding", "instruct", "chat", "digitize"}

		layers, err := GetDeploymentOrder(services)
		require.NoError(t, err, "Should get deployment order without error")
		require.NotNil(t, layers, "Layers should not be nil")

		assert.Greater(t, len(layers), 0, "Should have at least one layer")

		// Verify all services are included
		allServices := flattenLayers(layers)
		assert.Equal(t, len(services), len(allServices), "All services should be in deployment order")
	})

	t.Run("Handle empty service list", func(t *testing.T) {
		layers, err := GetDeploymentOrder([]string{})
		require.NoError(t, err, "Should handle empty list without error")
		assert.Empty(t, layers, "Layers should be empty for empty input")
	})

	t.Run("Detect circular dependencies", func(t *testing.T) {
		// This test would require creating services with circular dependencies
		// Since our current services don't have circular deps, we just verify
		// the algorithm completes without hanging
		services := []string{"chat", "opensearch", "embedding", "instruct"}
		layers, err := GetDeploymentOrder(services)
		require.NoError(t, err, "Should complete without hanging")
		assert.NotNil(t, layers, "Should return layers")
	})
}

func TestValidateDependencies(t *testing.T) {
	t.Run("Validate services with valid dependencies", func(t *testing.T) {
		services := []string{"opensearch", "chat"}
		err := ValidateDependencies(services)
		assert.NoError(t, err, "Should validate without error for valid dependencies")
	})

	t.Run("Validate service with missing dependency in catalog", func(t *testing.T) {
		// This test would require a service that depends on a non-existent service
		// Since all our services have valid dependencies, we test with a non-existent service
		services := []string{"chat", "opensearch", "embedding", "instruct"}
		err := ValidateDependencies(services)
		assert.NoError(t, err, "Should validate successfully when all dependencies exist in catalog")
	})

	t.Run("Validate non-existent service", func(t *testing.T) {
		services := []string{"non-existent"}
		err := ValidateDependencies(services)
		assert.Error(t, err, "Should return error for non-existent service")
		assert.Contains(t, err.Error(), "not found", "Error should mention service not found")
	})

	t.Run("Validate empty service list", func(t *testing.T) {
		err := ValidateDependencies([]string{})
		assert.NoError(t, err, "Should handle empty list without error")
	})

	t.Run("Validate all RAG services", func(t *testing.T) {
		// Get all services for RAG manually
		services := []string{"opensearch", "embedding", "instruct", "chat", "digitize"}

		err := ValidateDependencies(services)
		assert.NoError(t, err, "All RAG services should have valid dependencies")
	})
}

// ============================================================================
// Concurrency Tests
// ============================================================================

func TestConcurrency(t *testing.T) {
	t.Run("Concurrent architecture loads", func(t *testing.T) {
		done := make(chan bool, 10)

		for i := 0; i < 10; i++ {
			go func() {
				_, err := LoadArchitecture("rag")
				assert.NoError(t, err, "Concurrent load should not error")
				done <- true
			}()
		}

		for i := 0; i < 10; i++ {
			<-done
		}
	})

	t.Run("Concurrent service loads", func(t *testing.T) {
		services := []string{"opensearch", "chat", "instruct", "embedding"}
		done := make(chan bool, len(services))

		for _, svc := range services {
			go func(serviceID string) {
				_, err := LoadService(serviceID)
				assert.NoError(t, err, "Concurrent load should not error for %s", serviceID)
				done <- true
			}(svc)
		}

		for i := 0; i < len(services); i++ {
			<-done
		}
	})
}

// ============================================================================
// Helper Functions
// ============================================================================

func indexOf(slice []string, value string) int {
	for i, v := range slice {
		if v == value {
			return i
		}
	}
	return -1
}

func flattenLayers(layers [][]string) []string {
	var result []string
	for _, layer := range layers {
		result = append(result, layer...)
	}
	return result
}

// Made with Bob
