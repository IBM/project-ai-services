package types

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestArchitectureAboutOrderPreservation(t *testing.T) {
	// Sample YAML with ordered about field
	yamlData := `
id: test-arch
name: Test Architecture
description: Test
version: 1.0.0
type: architecture
certified_by: IBM
runtimes:
  - podman
services:
  - id: test-service
about:
  - title: "First Section"
    sections:
      - title: "First Item"
        value: "Value 1"
      - title: "Second Item"
        value: "Value 2"
  - title: "Second Section"
    sections:
      - title: "Third Item"
        value: "Value 3"
`

	var arch Architecture
	err := yaml.Unmarshal([]byte(yamlData), &arch)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(arch)
	if err != nil {
		t.Fatalf("Failed to marshal to JSON: %v", err)
	}

	jsonStr := string(jsonData)
	t.Logf("JSON output: %s", jsonStr)

	// Verify that "First Section" appears before "Second Section"
	firstSectionPos := findSubstring(jsonStr, "First Section")
	secondSectionPos := findSubstring(jsonStr, "Second Section")

	if firstSectionPos == -1 || secondSectionPos == -1 {
		t.Fatal("Expected sections not found in JSON output")
	}

	if firstSectionPos >= secondSectionPos {
		t.Errorf("Order not preserved: 'First Section' at position %d, 'Second Section' at position %d",
			firstSectionPos, secondSectionPos)
	}

	// Verify that "First Item" appears before "Second Item"
	firstItemPos := findSubstring(jsonStr, "First Item")
	secondItemPos := findSubstring(jsonStr, "Second Item")

	if firstItemPos == -1 || secondItemPos == -1 {
		t.Fatal("Expected items not found in JSON output")
	}

	if firstItemPos >= secondItemPos {
		t.Errorf("Order not preserved: 'First Item' at position %d, 'Second Item' at position %d",
			firstItemPos, secondItemPos)
	}
}

func TestServiceAboutOrderPreservation(t *testing.T) {
	// Sample YAML with ordered about field
	yamlData := `
id: test-service
name: Test Service
description: Test
type: service
certified_by: IBM
architectures:
  - test-arch
about:
  - title: "Service details"
    sections:
      - title: "Version"
        value: "1.0.0"
      - title: "Model"
        value: "test-model"
  - title: "Inputs and outputs"
    sections:
      - title: "Inputs"
        values:
          - "Input 1"
          - "Input 2"
`

	var svc Service
	err := yaml.Unmarshal([]byte(yamlData), &svc)
	if err != nil {
		t.Fatalf("Failed to unmarshal YAML: %v", err)
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(svc)
	if err != nil {
		t.Fatalf("Failed to marshal to JSON: %v", err)
	}

	jsonStr := string(jsonData)
	t.Logf("JSON output: %s", jsonStr)

	// Verify that "Service details" appears before "Inputs and outputs"
	serviceDetailsPos := findSubstring(jsonStr, "Service details")
	inputsOutputsPos := findSubstring(jsonStr, "Inputs and outputs")

	if serviceDetailsPos == -1 || inputsOutputsPos == -1 {
		t.Fatal("Expected sections not found in JSON output")
	}

	if serviceDetailsPos >= inputsOutputsPos {
		t.Errorf("Order not preserved: 'Service details' at position %d, 'Inputs and outputs' at position %d",
			serviceDetailsPos, inputsOutputsPos)
	}

	// Verify that "Version" appears before "Model"
	versionPos := findSubstring(jsonStr, "Version")
	modelPos := findSubstring(jsonStr, "Model")

	if versionPos == -1 || modelPos == -1 {
		t.Fatal("Expected items not found in JSON output")
	}

	if versionPos >= modelPos {
		t.Errorf("Order not preserved: 'Version' at position %d, 'Model' at position %d",
			versionPos, modelPos)
	}
}

// Helper function to find substring position
func findSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// Made with Bob
