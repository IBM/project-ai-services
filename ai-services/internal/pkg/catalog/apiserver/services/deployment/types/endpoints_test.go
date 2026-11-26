package types

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestMergeEndpoints(t *testing.T) {
	newPlan := func(values map[string]any) *DeploymentPlan {
		return &DeploymentPlan{Services: map[string]*ServicePlan{"chat": {CatalogID: "chat", Values: values}}}
	}
	comp := &ComponentPlan{
		ComponentType:  "llm",
		DatabaseID:     uuid.New(),
		UsedByServices: []string{"chat", "unknown"},
	}
	assert := assert.New(t)
	withEndpoint := func() ComponentPlan {
		c := *comp
		c.Endpoints = map[string]any{"llm": map[string]any{"host": "10.0.0.5", "port": "8000"}}

		return c
	}

	t.Run("sets the endpoint when the service has no values for the component", func(t *testing.T) {
		c := withEndpoint()
		plan := newPlan(nil)

		plan.MergeEndpoints(t.Context(), &c)

		got, _ := plan.Services["chat"].Values["llm"].(map[string]any)
		assert.Equal("10.0.0.5", got["host"])
		assert.Equal("8000", got["port"])
		assert.NotEmpty(got["instanceSlug"], "instanceSlug must be present even when the service had no prior values")
	})

	t.Run("merges into the values the service already has and adds the instance slug", func(t *testing.T) {
		c := withEndpoint()
		plan := newPlan(map[string]any{"llm": map[string]any{"model": "ibm/granite"}})

		plan.MergeEndpoints(t.Context(), &c)

		got, _ := plan.Services["chat"].Values["llm"].(map[string]any)
		assert.Equal("ibm/granite", got["model"])
		assert.Equal("10.0.0.5", got["host"])
		assert.Equal("8000", got["port"])
		assert.NotEmpty(got["instanceSlug"])
	})

	t.Run("the service's values do not alias the component's endpoints", func(t *testing.T) {
		c := withEndpoint()
		plan := newPlan(nil)

		plan.MergeEndpoints(t.Context(), &c)
		plan.Services["chat"].Values["llm"].(map[string]any)["extra"] = "changed"

		// The component's own endpoint map must not be mutated by writes to the service's copy.
		endpointLLM, _ := c.Endpoints["llm"].(map[string]any)
		assert.NotContains(endpointLLM, "extra")
	})

	t.Run("a component without endpoints changes nothing", func(t *testing.T) {
		plan := newPlan(map[string]any{})

		plan.MergeEndpoints(t.Context(), comp)

		assert.Empty(plan.Services["chat"].Values)
	})
}

func TestMergeComponentValues(t *testing.T) {
	assert := assert.New(t)

	t.Run("merges into the values the service has and keeps the others", func(t *testing.T) {
		plan := &DeploymentPlan{Services: map[string]*ServicePlan{"chat": {Values: map[string]any{
			"llm": map[string]any{"host": "model-server", "model": "granite"},
		}}}}

		plan.MergeComponentValues(t.Context(), "chat", "llm", map[string]any{"host": "litellm", "key": "sk-1"})

		assert.Equal(map[string]any{"host": "litellm", "model": "granite", "key": "sk-1"}, plan.Services["chat"].Values["llm"])
	})

	t.Run("creates the values of a service that has none, without sharing the map", func(t *testing.T) {
		plan := &DeploymentPlan{Services: map[string]*ServicePlan{"chat": {}, "summarize": {}}}
		values := map[string]any{"host": "litellm"}

		plan.MergeComponentValues(t.Context(), "chat", "llm", values)
		plan.MergeComponentValues(t.Context(), "summarize", "llm", values)
		plan.Services["chat"].Values["llm"].(map[string]any)["key"] = "sk-chat"

		assert.NotContains(plan.Services["summarize"].Values["llm"], "key")
		assert.NotContains(values, "key")
	})

	t.Run("ignores a service that is not in the plan", func(t *testing.T) {
		plan := &DeploymentPlan{Services: map[string]*ServicePlan{}}

		plan.MergeComponentValues(t.Context(), "gone", "llm", map[string]any{"host": "litellm"})

		assert.Empty(plan.Services)
	})

	t.Run("is safe to call concurrently", func(t *testing.T) {
		plan := &DeploymentPlan{Services: map[string]*ServicePlan{"chat": {}}}

		var wg sync.WaitGroup
		for _, componentType := range []string{"llm", "embedding", "reranker"} {
			wg.Add(1)

			go func() {
				defer wg.Done()
				plan.MergeComponentValues(t.Context(), "chat", componentType, map[string]any{"host": "litellm"})
			}()
		}

		wg.Wait()

		assert.Len(plan.Services["chat"].Values, 3)
	})
}
