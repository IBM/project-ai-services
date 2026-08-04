package application

import (
	"testing"

	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
)

func TestExtractProviderParams_FalseSkipsProvider(t *testing.T) {
	allParams := map[string]string{
		"llm.vllm-cpu": "false",
	}
	providerParams := make(map[string]map[string]string)

	extractProviderParams("llm.", allParams, providerParams, make(map[string]bool))

	if _, exists := providerParams["vllm-cpu"]; exists {
		t.Error("expected vllm-cpu to be skipped when value is 'false'")
	}
}

func TestExtractProviderParams_TrueSelectsProvider(t *testing.T) {
	allParams := map[string]string{
		"llm.vllm-cpu": "true",
	}
	providerParams := make(map[string]map[string]string)

	extractProviderParams("llm.", allParams, providerParams, make(map[string]bool))

	if _, exists := providerParams["vllm-cpu"]; !exists {
		t.Error("expected vllm-cpu to be selected when value is 'true'")
	}
}

func TestExtractProviderParams_FalseCaseInsensitive(t *testing.T) {
	allParams := map[string]string{
		"llm.vllm-cpu": "False",
	}
	providerParams := make(map[string]map[string]string)

	extractProviderParams("llm.", allParams, providerParams, make(map[string]bool))

	if _, exists := providerParams["vllm-cpu"]; exists {
		t.Error("expected vllm-cpu to be skipped when value is 'False' (case insensitive)")
	}
}

func TestExtractProviderParams_TrueCaseInsensitive(t *testing.T) {
	allParams := map[string]string{
		"llm.vllm-cpu": "True",
	}
	providerParams := make(map[string]map[string]string)

	extractProviderParams("llm.", allParams, providerParams, make(map[string]bool))

	if _, exists := providerParams["vllm-cpu"]; !exists {
		t.Error("expected vllm-cpu to be selected when value is 'True' (case insensitive)")
	}
}

func TestExtractProviderParams_InvalidValueTreatedAsFalse(t *testing.T) {
	testCases := []struct {
		name  string
		value string
	}{
		{"empty string", ""},
		{"arbitrary string", "yes"},
		{"numeric", "1"},
		{"typo", "tru"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			allParams := map[string]string{
				"llm.vllm-cpu": tc.value,
			}
			providerParams := make(map[string]map[string]string)

			extractProviderParams("llm.", allParams, providerParams, make(map[string]bool))

			if _, exists := providerParams["vllm-cpu"]; exists {
				t.Errorf("expected vllm-cpu to be skipped for invalid value '%s' (treated as false)", tc.value)
			}
		})
	}
}

func TestExtractProviderParams_NestedParamNotAffectedByFalse(t *testing.T) {
	// A nested param like llm.vllm-cpu.model=false should still be treated as a param value.
	allParams := map[string]string{
		"llm.vllm-cpu.model": "false",
	}
	providerParams := make(map[string]map[string]string)

	extractProviderParams("llm.", allParams, providerParams, make(map[string]bool))

	if _, exists := providerParams["vllm-cpu"]; !exists {
		t.Fatal("expected vllm-cpu to exist with nested param")
	}

	if providerParams["vllm-cpu"]["model"] != "false" {
		t.Errorf("expected model param to be 'false', got '%s'", providerParams["vllm-cpu"]["model"])
	}
}

func TestExtractProviderParams_AllFalseResultsInEmpty(t *testing.T) {
	allParams := map[string]string{
		"llm.vllm-cpu":   "false",
		"llm.vllm-spyre": "false",
	}
	providerParams := make(map[string]map[string]string)

	extractProviderParams("llm.", allParams, providerParams, make(map[string]bool))

	if len(providerParams) != 0 {
		t.Errorf("expected empty providerParams when all providers are false, got %v", providerParams)
	}
}

func TestExtractComponentParamsForService_OneTrueOneFalse(t *testing.T) {
	// vllm-spyre=false alongside vllm-cpu=true — no warning, vllm-cpu selected.
	allParams := map[string]string{
		"llm.vllm-spyre": "false",
		"llm.vllm-cpu":   "true",
	}

	providerParams := extractComponentParamsForService("chat", "llm", allParams)

	if _, exists := providerParams["vllm-spyre"]; exists {
		t.Error("expected vllm-spyre to be skipped")
	}

	if _, exists := providerParams["vllm-cpu"]; !exists {
		t.Error("expected vllm-cpu to be selected")
	}
}

func TestExtractComponentParamsForService_OnlyFalse_DefaultApplies(t *testing.T) {
	// vllm-spyre=false with no other provider selected — warning logged, providerParams empty so default kicks in.
	allParams := map[string]string{
		"llm.vllm-spyre": "false",
	}

	providerParams := extractComponentParamsForService("chat", "llm", allParams)

	if len(providerParams) != 0 {
		t.Errorf("expected empty providerParams so default is used, got %v", providerParams)
	}
}

func TestFindUserSpecifiedProvider_SingleProvider(t *testing.T) {
	compDeployOpt := catalogTypes.DeployOptionsComponent{
		Type: "llm",
		Providers: []catalogTypes.DeployOptionsProvider{
			{ID: "vllm-cpu"},
			{ID: "vllm-spyre"},
		},
	}
	providerParams := map[string]map[string]string{
		"vllm-cpu": {},
	}

	providerID, params, err := findUserSpecifiedProvider(compDeployOpt, providerParams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if providerID != "vllm-cpu" {
		t.Errorf("expected 'vllm-cpu', got '%s'", providerID)
	}

	if params == nil {
		t.Error("expected non-nil params")
	}
}

func TestFindUserSpecifiedProvider_MultipleProviders_ReturnsError(t *testing.T) {
	compDeployOpt := catalogTypes.DeployOptionsComponent{
		Type: "llm",
		Providers: []catalogTypes.DeployOptionsProvider{
			{ID: "vllm-cpu"},
			{ID: "vllm-spyre"},
		},
	}
	providerParams := map[string]map[string]string{
		"vllm-cpu":   {},
		"vllm-spyre": {},
	}

	_, _, err := findUserSpecifiedProvider(compDeployOpt, providerParams)
	if err == nil {
		t.Fatal("expected error when multiple providers specified, got nil")
	}
}

func TestFindUserSpecifiedProvider_NoMatchingProvider(t *testing.T) {
	compDeployOpt := catalogTypes.DeployOptionsComponent{
		Type: "llm",
		Providers: []catalogTypes.DeployOptionsProvider{
			{ID: "vllm-cpu"},
			{ID: "vllm-spyre"},
		},
	}
	providerParams := map[string]map[string]string{
		"watsonx": {"model": "ibm/granite"},
	}

	providerID, _, err := findUserSpecifiedProvider(compDeployOpt, providerParams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if providerID != "" {
		t.Errorf("expected empty provider ID for non-matching provider, got '%s'", providerID)
	}
}

func TestSelectProviderFromDeployOptions_FallsBackToSpyre(t *testing.T) {
	compDeployOpt := catalogTypes.DeployOptionsComponent{
		Type: "llm",
		Providers: []catalogTypes.DeployOptionsProvider{
			{ID: "vllm-cpu"},
			{ID: "vllm-spyre"},
		},
	}
	// Empty providerParams — no user selection.
	providerParams := map[string]map[string]string{}

	providerID, _, err := selectProviderFromDeployOptions(compDeployOpt, providerParams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if providerID != "vllm-spyre" {
		t.Errorf("expected 'vllm-spyre' as default, got '%s'", providerID)
	}
}

func TestSelectProviderFromDeployOptions_FallsBackToDefault(t *testing.T) {
	compDeployOpt := catalogTypes.DeployOptionsComponent{
		Type: "embedding",
		Providers: []catalogTypes.DeployOptionsProvider{
			{ID: "vllm-cpu"},
			{ID: "tei", Default: true},
		},
	}
	providerParams := map[string]map[string]string{}

	providerID, _, err := selectProviderFromDeployOptions(compDeployOpt, providerParams)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if providerID != "tei" {
		t.Errorf("expected 'tei' as default provider, got '%s'", providerID)
	}
}
