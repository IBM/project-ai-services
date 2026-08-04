package catalog

import (
	"context"
	"errors"
	"testing"
	texttemplate "text/template"

	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProcessTemplates_Success tests successful template processing.
func TestProcessTemplates_Success(t *testing.T) {
	provider := &CatalogProvider{}

	// Create a simple pod template
	templateContent := `
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  containers:
  - name: test-container
    image: {{ .Values.image }}
  initContainers:
  - name: init-container
    image: {{ .Values.initImage }}
`

	tmpl, err := texttemplate.New("test").Parse(templateContent)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"test-template": tmpl,
	}

	values := map[string]any{
		"image":     "nginx:latest",
		"initImage": "busybox:latest",
	}

	// Track processed templates
	processedTemplates := make(map[string]*models.PodSpec)

	processor := func(templateName string, podSpec *models.PodSpec) error {
		processedTemplates[templateName] = podSpec

		return nil
	}

	err = provider.ProcessTemplates(context.Background(), templates, values, "test-instance", processor)
	require.NoError(t, err)

	// Verify the template was processed
	assert.Len(t, processedTemplates, 1)
	assert.Contains(t, processedTemplates, "test-template")

	podSpec := processedTemplates["test-template"]
	assert.Equal(t, "test-pod", podSpec.Name)
	assert.Len(t, podSpec.Spec.Containers, 1)
	assert.Equal(t, "nginx:latest", podSpec.Spec.Containers[0].Image)
	assert.Len(t, podSpec.Spec.InitContainers, 1)
	assert.Equal(t, "busybox:latest", podSpec.Spec.InitContainers[0].Image)
}

// TestProcessTemplates_MultipleTemplates tests processing multiple templates.
func TestProcessTemplates_MultipleTemplates(t *testing.T) {
	provider := &CatalogProvider{}

	template1 := `
apiVersion: v1
kind: Pod
metadata:
  name: pod1
spec:
  containers:
  - name: container1
    image: image1:latest
`

	template2 := `
apiVersion: v1
kind: Pod
metadata:
  name: pod2
spec:
  containers:
  - name: container2
    image: image2:latest
`

	tmpl1, err := texttemplate.New("template1").Parse(template1)
	require.NoError(t, err)

	tmpl2, err := texttemplate.New("template2").Parse(template2)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"template1": tmpl1,
		"template2": tmpl2,
	}

	processedCount := 0
	processor := func(templateName string, podSpec *models.PodSpec) error {
		processedCount++

		return nil
	}

	err = provider.ProcessTemplates(context.Background(), templates, nil, "test-instance", processor)
	require.NoError(t, err)
	assert.Equal(t, 2, processedCount)
}

// TestProcessTemplates_RenderError tests handling of template rendering errors.
func TestProcessTemplates_RenderError(t *testing.T) {
	provider := &CatalogProvider{}

	// Template with undefined variable
	templateContent := `
apiVersion: v1
kind: Pod
metadata:
	 name: {{ .UndefinedVariable }}
`

	tmpl, err := texttemplate.New("test").Option("missingkey=error").Parse(templateContent)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"bad-template": tmpl,
	}

	processor := func(templateName string, podSpec *models.PodSpec) error {
		return nil
	}

	err = provider.ProcessTemplates(context.Background(), templates, nil, "test-instance", processor)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process")
}

// TestProcessTemplates_ParseError tests handling of YAML parsing errors.
func TestProcessTemplates_ParseError(t *testing.T) {
	provider := &CatalogProvider{}

	// Template that produces invalid YAML
	templateContent := `
this is not valid yaml: [[[
`

	tmpl, err := texttemplate.New("test").Parse(templateContent)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"invalid-yaml": tmpl,
	}

	processor := func(templateName string, podSpec *models.PodSpec) error {
		return nil
	}

	err = provider.ProcessTemplates(context.Background(), templates, nil, "test-instance", processor)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process")
}

// TestProcessTemplates_ProcessorError tests handling of processor callback errors.
func TestProcessTemplates_ProcessorError(t *testing.T) {
	provider := &CatalogProvider{}

	templateContent := `
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  containers:
  - name: test-container
    image: nginx:latest
`

	tmpl, err := texttemplate.New("test").Parse(templateContent)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"test-template": tmpl,
	}

	expectedError := errors.New("processor failed")
	processor := func(templateName string, podSpec *models.PodSpec) error {
		return expectedError
	}

	err = provider.ProcessTemplates(context.Background(), templates, nil, "test-instance", processor)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process template test-template")
	assert.ErrorIs(t, err, expectedError)
}

// TestProcessTemplates_MixedErrors tests handling when some templates fail and some succeed.
func TestProcessTemplates_MixedErrors(t *testing.T) {
	provider := &CatalogProvider{}

	goodTemplate := `
apiVersion: v1
kind: Pod
metadata:
  name: good-pod
spec:
  containers:
  - name: container
    image: nginx:latest
`

	badTemplate := `
this is invalid yaml
`

	tmpl1, err := texttemplate.New("good").Parse(goodTemplate)
	require.NoError(t, err)

	tmpl2, err := texttemplate.New("bad").Parse(badTemplate)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"good-template": tmpl1,
		"bad-template":  tmpl2,
	}

	processedCount := 0
	processor := func(templateName string, podSpec *models.PodSpec) error {
		processedCount++

		return nil
	}

	err = provider.ProcessTemplates(context.Background(), templates, nil, "test-instance", processor)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process")
	// Good template should still be processed
	assert.Equal(t, 1, processedCount)
}

// TestCollectImagesFromTemplates_Success tests successful image collection.
func TestCollectImagesFromTemplates_Success(t *testing.T) {
	provider := &CatalogProvider{}

	templateContent := `
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  containers:
  - name: app
    image: nginx:1.21
  - name: sidecar
    image: busybox:latest
  initContainers:
  - name: init
    image: alpine:3.14
`

	tmpl, err := texttemplate.New("test").Parse(templateContent)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"test-template": tmpl,
	}

	imageSet := make(map[string]bool)

	err = provider.CollectImagesFromTemplates(context.Background(), templates, nil, imageSet)
	require.NoError(t, err)

	// Verify all images were collected
	assert.Len(t, imageSet, 3)
	assert.True(t, imageSet["nginx:1.21"])
	assert.True(t, imageSet["busybox:latest"])
	assert.True(t, imageSet["alpine:3.14"])
}

// TestCollectImagesFromTemplates_EmptyImages tests handling of pods without images.
func TestCollectImagesFromTemplates_EmptyImages(t *testing.T) {
	provider := &CatalogProvider{}

	templateContent := `
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  containers: []
`

	tmpl, err := texttemplate.New("test").Parse(templateContent)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"test-template": tmpl,
	}

	imageSet := make(map[string]bool)

	err = provider.CollectImagesFromTemplates(context.Background(), templates, nil, imageSet)
	require.NoError(t, err)
	assert.Empty(t, imageSet)
}

// TestCollectImagesFromTemplates_Deduplication tests that duplicate images are deduplicated.
func TestCollectImagesFromTemplates_Deduplication(t *testing.T) {
	provider := &CatalogProvider{}

	template1 := `
apiVersion: v1
kind: Pod
metadata:
  name: pod1
spec:
  containers:
  - name: container1
    image: nginx:latest
`

	template2 := `
apiVersion: v1
kind: Pod
metadata:
  name: pod2
spec:
  containers:
  - name: container2
    image: nginx:latest
`

	tmpl1, err := texttemplate.New("template1").Parse(template1)
	require.NoError(t, err)

	tmpl2, err := texttemplate.New("template2").Parse(template2)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"template1": tmpl1,
		"template2": tmpl2,
	}

	imageSet := make(map[string]bool)

	err = provider.CollectImagesFromTemplates(context.Background(), templates, nil, imageSet)
	require.NoError(t, err)

	// Should only have one entry despite two templates using the same image
	assert.Len(t, imageSet, 1)
	assert.True(t, imageSet["nginx:latest"])
}

// TestCollectImagesFromTemplates_WithValues tests image collection with template values.
func TestCollectImagesFromTemplates_WithValues(t *testing.T) {
	provider := &CatalogProvider{}

	templateContent := `
apiVersion: v1
kind: Pod
metadata:
  name: test-pod
spec:
  containers:
  - name: app
    image: {{ .Values.appImage }}
  initContainers:
  - name: init
    image: {{ .Values.initImage }}
`

	tmpl, err := texttemplate.New("test").Parse(templateContent)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"test-template": tmpl,
	}

	values := map[string]any{
		"appImage":  "myapp:v1.0.0",
		"initImage": "myinit:v2.0.0",
	}

	imageSet := make(map[string]bool)

	err = provider.CollectImagesFromTemplates(context.Background(), templates, values, imageSet)
	require.NoError(t, err)

	assert.Len(t, imageSet, 2)
	assert.True(t, imageSet["myapp:v1.0.0"])
	assert.True(t, imageSet["myinit:v2.0.0"])
}

// TestCollectImagesFromTemplates_Error tests error handling in image collection.
func TestCollectImagesFromTemplates_Error(t *testing.T) {
	provider := &CatalogProvider{}

	// Template with invalid YAML
	templateContent := `
invalid yaml content [[[
`

	tmpl, err := texttemplate.New("test").Parse(templateContent)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"bad-template": tmpl,
	}

	imageSet := make(map[string]bool)

	err = provider.CollectImagesFromTemplates(context.Background(), templates, nil, imageSet)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process")
}

// Made with Bob

// TestCollectSpyreCardsFromTemplates_WithRealTemplates tests Spyre card collection using real templates from assets.
func TestCollectSpyreCardsFromTemplates_WithRealTemplates(t *testing.T) {
	provider := &CatalogProvider{}

	// Load real vllm-spyre template (has 4 Spyre cards)
	llmTemplateContent := `apiVersion: v1
kind: Pod
metadata:
  name: "llm-{{ .InstanceSlug }}"
  labels:
    ai-services.io/template: "{{ .TemplateID }}"
  annotations:
    io.podman.annotations.ulimit: "nofile=134217728:134217728,memlock=-1:-1"
    ai-services.io/llm--spyre-cards: "4"
spec:
  containers:
    - name: llm
      image: "{{ .Values.image }}"
      resources:
        requests:
          podman.io/device=/dev/vfio: 4
          memory: "150Gi"
`

	// Load real reranker-spyre template (has 1 Spyre card)
	rerankerTemplateContent := `apiVersion: v1
kind: Pod
metadata:
  name: "reranker-{{ .InstanceSlug }}"
  labels:
    ai-services.io/template: "{{ .TemplateID }}"
  annotations:
    io.podman.annotations.ulimit: "nofile=134217728:134217728,memlock=-1:-1"
    ai-services.io/reranker--spyre-cards: "1"
spec:
  containers:
    - name: reranker
      image: "{{ .Values.image }}"
      resources:
        requests:
          podman.io/device=/dev/vfio: 1
          memory: "5Gi"
`

	llmTmpl, err := texttemplate.New("vllm-server").Parse(llmTemplateContent)
	require.NoError(t, err)

	rerankerTmpl, err := texttemplate.New("reranker-server").Parse(rerankerTemplateContent)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"vllm-server.yaml.tmpl":     llmTmpl,
		"reranker-server.yaml.tmpl": rerankerTmpl,
	}

	values := map[string]any{
		"image": "registry.redhat.io/rhaii/vllm-spyre-rhel9:3.4.0",
	}

	totalCards, err := provider.CollectSpyreCardsFromTemplates(context.Background(), templates, values)
	require.NoError(t, err)

	// Should have 4 + 1 = 5 total Spyre cards
	assert.Equal(t, 5, totalCards)
}

// TestCollectSpyreCardsFromTemplates_NoSpyreCards tests when templates have no Spyre card annotations.
func TestCollectSpyreCardsFromTemplates_NoSpyreCards(t *testing.T) {
	provider := &CatalogProvider{}

	// Template without Spyre card annotations
	templateContent := `apiVersion: v1
kind: Pod
metadata:
  name: test-pod
  annotations:
    some.other/annotation: "value"
spec:
  containers:
  - name: test-container
    image: nginx:latest
`

	tmpl, err := texttemplate.New("test").Parse(templateContent)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"test-template": tmpl,
	}

	values := map[string]any{}

	totalCards, err := provider.CollectSpyreCardsFromTemplates(context.Background(), templates, values)
	require.NoError(t, err)

	// Should have 0 Spyre cards
	assert.Equal(t, 0, totalCards)
}

// TestCollectSpyreCardsFromTemplates_MultipleContainers tests multiple containers with different Spyre card counts.
func TestCollectSpyreCardsFromTemplates_MultipleContainers(t *testing.T) {
	provider := &CatalogProvider{}

	templateContent := `apiVersion: v1
kind: Pod
metadata:
  name: multi-container-pod
  annotations:
    ai-services.io/container1--spyre-cards: "2"
    ai-services.io/container2--spyre-cards: "3"
    ai-services.io/container3--spyre-cards: "1"
spec:
  containers:
  - name: container1
    image: image1:latest
  - name: container2
    image: image2:latest
  - name: container3
    image: image3:latest
`

	tmpl, err := texttemplate.New("multi").Parse(templateContent)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"multi-template": tmpl,
	}

	values := map[string]any{}

	totalCards, err := provider.CollectSpyreCardsFromTemplates(context.Background(), templates, values)
	require.NoError(t, err)

	// Should have 2 + 3 + 1 = 6 total Spyre cards
	assert.Equal(t, 6, totalCards)
}

// TestCollectSpyreCardsFromTemplates_InvalidAnnotationValue tests error handling for invalid annotation values.
func TestCollectSpyreCardsFromTemplates_InvalidAnnotationValue(t *testing.T) {
	provider := &CatalogProvider{}

	templateContent := `apiVersion: v1
kind: Pod
metadata:
  name: invalid-pod
  annotations:
    ai-services.io/container1--spyre-cards: "not-a-number"
spec:
  containers:
  - name: container1
    image: image1:latest
`

	tmpl, err := texttemplate.New("invalid").Parse(templateContent)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"invalid-template": tmpl,
	}

	values := map[string]any{}

	totalCards, err := provider.CollectSpyreCardsFromTemplates(context.Background(), templates, values)

	// Should return an error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to convert to int")
	assert.Equal(t, 0, totalCards)
}

// TestCollectSpyreCardsFromTemplates_MixedTemplates tests a mix of templates with and without Spyre cards.
func TestCollectSpyreCardsFromTemplates_MixedTemplates(t *testing.T) {
	provider := &CatalogProvider{}

	// Template with Spyre cards
	template1Content := `apiVersion: v1
kind: Pod
metadata:
  name: spyre-pod
  annotations:
    ai-services.io/llm--spyre-cards: "4"
spec:
  containers:
  - name: llm
    image: vllm:latest
`

	// Template without Spyre cards
	template2Content := `apiVersion: v1
kind: Pod
metadata:
  name: regular-pod
spec:
  containers:
  - name: app
    image: app:latest
`

	// Another template with Spyre cards
	template3Content := `apiVersion: v1
kind: Pod
metadata:
  name: another-spyre-pod
  annotations:
    ai-services.io/reranker--spyre-cards: "2"
spec:
  containers:
  - name: reranker
    image: reranker:latest
`

	tmpl1, err := texttemplate.New("spyre1").Parse(template1Content)
	require.NoError(t, err)

	tmpl2, err := texttemplate.New("regular").Parse(template2Content)
	require.NoError(t, err)

	tmpl3, err := texttemplate.New("spyre2").Parse(template3Content)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"spyre-template1":  tmpl1,
		"regular-template": tmpl2,
		"spyre-template2":  tmpl3,
	}

	values := map[string]any{}

	totalCards, err := provider.CollectSpyreCardsFromTemplates(context.Background(), templates, values)
	require.NoError(t, err)

	// Should have 4 + 0 + 2 = 6 total Spyre cards
	assert.Equal(t, 6, totalCards)
}

// TestCollectSpyreCardsFromTemplates_TemplateRenderError tests error handling when template rendering fails.
func TestCollectSpyreCardsFromTemplates_TemplateRenderError(t *testing.T) {
	provider := &CatalogProvider{}

	// Template with undefined variable that will cause render error
	templateContent := `apiVersion: v1
kind: Pod
metadata:
  name: {{ .UndefinedVariable }}
  annotations:
    ai-services.io/llm--spyre-cards: "4"
spec:
  containers:
  - name: test
    image: test:latest
`

	tmpl, err := texttemplate.New("error").Option("missingkey=error").Parse(templateContent)
	require.NoError(t, err)

	templates := map[string]*texttemplate.Template{
		"error-template": tmpl,
	}

	values := map[string]any{}

	totalCards, err := provider.CollectSpyreCardsFromTemplates(context.Background(), templates, values)

	// Should return an error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to process")
	assert.Equal(t, 0, totalCards)
}

// getSpyreCardTestCases returns test cases for Spyre card annotation parsing.
//
//nolint:funlen // Test data structure is intentionally long for comprehensive coverage
func getSpyreCardTestCases() []struct {
	name                 string
	annotations          map[string]string
	expectedTotal        int
	expectedContainerMap map[string]int
	expectError          bool
} {
	return []struct {
		name                 string
		annotations          map[string]string
		expectedTotal        int
		expectedContainerMap map[string]int
		expectError          bool
	}{
		{
			name: "single container with Spyre cards",
			annotations: map[string]string{
				"ai-services.io/llm--spyre-cards": "4",
			},
			expectedTotal: 4,
			expectedContainerMap: map[string]int{
				"llm": 4,
			},
			expectError: false,
		},
		{
			name: "multiple containers with Spyre cards",
			annotations: map[string]string{
				"ai-services.io/llm--spyre-cards":      "4",
				"ai-services.io/reranker--spyre-cards": "1",
			},
			expectedTotal: 5,
			expectedContainerMap: map[string]int{
				"llm":      4,
				"reranker": 1,
			},
			expectError: false,
		},
		{
			name: "no Spyre card annotations",
			annotations: map[string]string{
				"some.other/annotation": "value",
			},
			expectedTotal:        0,
			expectedContainerMap: map[string]int{},
			expectError:          false,
		},
		{
			name: "invalid annotation value",
			annotations: map[string]string{
				"ai-services.io/llm--spyre-cards": "not-a-number",
			},
			expectedTotal:        0,
			expectedContainerMap: map[string]int{},
			expectError:          true,
		},
		{
			name: "container name with special characters",
			annotations: map[string]string{
				"ai-services.io/my-container-123--spyre-cards": "2",
			},
			expectedTotal: 2,
			expectedContainerMap: map[string]int{
				"my-container-123": 2,
			},
			expectError: false,
		},
	}
}

// TestFetchSpyreCardsFromPodAnnotations tests the helper function directly.
func TestFetchSpyreCardsFromPodAnnotations(t *testing.T) {
	tests := getSpyreCardTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, containerMap, err := fetchSpyreCardsFromPodAnnotations(tt.annotations)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedTotal, total)
				assert.Equal(t, tt.expectedContainerMap, containerMap)
			}
		})
	}
}
