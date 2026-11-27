package bootstrap

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/project-ai-services/ai-services/internal/pkg/validators"
)

func ruleNames(rules []validators.Rule) []string {
	names := make([]string, 0, len(rules))
	for _, r := range rules {
		names = append(names, r.Name())
	}

	return names
}

func TestPodmanRules(t *testing.T) {
	spyreOnly := ruleNames(validators.PodmanRegistry.Rules())
	amdOnly := ruleNames(validators.ROCmRegistry.Rules())

	t.Run("no AMD GPU uses the Spyre rules", func(t *testing.T) {
		assert.Equal(t, spyreOnly, ruleNames(podmanRules(false, true)))
		assert.Equal(t, spyreOnly, ruleNames(podmanRules(false, false)))
	})

	t.Run("AMD-only uses the ROCm rules", func(t *testing.T) {
		assert.Equal(t, amdOnly, ruleNames(podmanRules(true, false)))
		assert.Contains(t, amdOnly, "rhel-rocm")
		assert.NotContains(t, amdOnly, "rhel")
	})

	t.Run("AMD and Spyre use every rule once", func(t *testing.T) {
		got := ruleNames(podmanRules(true, true))

		assert.ElementsMatch(t, got, dedupe(append(append([]string{}, spyreOnly...), amdOnly...)))
		assert.Contains(t, got, "amd-gpu")
		assert.Contains(t, got, "rocm-version")
		assert.Contains(t, got, "rhel-rocm")
		assert.Contains(t, got, "spyre")
	})

	t.Run("AllPodmanRules lists every rule once, independent of the host", func(t *testing.T) {
		got := ruleNames(AllPodmanRules())

		assert.ElementsMatch(t, got, dedupe(append(append([]string{}, spyreOnly...), amdOnly...)))
		assert.Contains(t, got, "amd-gpu")
		assert.Contains(t, got, "rocm-version")
		assert.Contains(t, got, "rhel-rocm")
	})

	t.Run("does not modify the registries", func(t *testing.T) {
		before := len(validators.PodmanRegistry.Rules())
		_ = podmanRules(true, true)
		assert.Len(t, validators.PodmanRegistry.Rules(), before)
	})
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	return out
}
