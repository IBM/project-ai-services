package opensearch

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	legacyMapping  = `{"rag":{"mappings":{"properties":{"chunk_id":{"type":"long"},"embedding":{"type":"knn_vector","dimension":768},"text":{"type":"text"},"metadata":{"properties":{"doc_id":{"type":"keyword"}}}}}}}`
	currentMapping = `{"rag":{"mappings":{"_meta":{"schema_version":2},"properties":{"chunk_id":{"type":"long"},"embedding":{"type":"knn_vector","dimension":768},"doc_id":{"type":"keyword"}}}}}`
	backupData     = `[{"_id":"1","_source":{"chunk_id":1,"text":"a","metadata":{"doc_id":"x","page_number":2}}},{"_id":"2","_source":{"chunk_id":2,"text":"b","doc_id":"y"}}]`
)

func requireJQ(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not installed")
	}
}

// runScriptFragment runs the given shell fragment with bash and returns its trimmed stdout.
func runScriptFragment(t *testing.T, fragment string) string {
	t.Helper()

	out, err := exec.Command("bash", "-c", fragment).CombinedOutput()
	if err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}

	return strings.TrimSpace(string(out))
}

// mappingFragment extracts the MAPPING=... assignment from the generated create-index script.
func mappingFragment(t *testing.T, dir string) string {
	t.Helper()

	script := GenerateCreateIndexScript("localhost:9200", dir, "rag")
	start := strings.Index(script, "MAPPING=")
	end := strings.Index(script, "SETTINGS=")
	if start < 0 || end < start {
		t.Fatalf("MAPPING assignment not found in script:\n%s", script)
	}

	return script[start:end]
}

func TestCreateIndexScriptUpgradesLegacyMapping(t *testing.T) {
	requireJQ(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rag_mapping.json"), []byte(legacyMapping), 0o600); err != nil {
		t.Fatal(err)
	}

	out := runScriptFragment(t, mappingFragment(t, dir)+`echo "$MAPPING" | jq -c '[._meta.schema_version, (.properties | has("metadata")), (.properties | has("doc_id")), .properties.embedding.dimension]'`)
	if out != "[2,false,true,768]" {
		t.Fatalf("unexpected upgraded mapping summary: %s", out)
	}
}

func TestCreateIndexScriptKeepsCurrentMapping(t *testing.T) {
	requireJQ(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rag_mapping.json"), []byte(currentMapping), 0o600); err != nil {
		t.Fatal(err)
	}

	out := runScriptFragment(t, mappingFragment(t, dir)+`echo "$MAPPING" | jq -c '.properties | keys'`)
	if out != `["chunk_id","doc_id","embedding"]` {
		t.Fatalf("current mapping must pass through unchanged, got: %s", out)
	}
}

func TestBulkIndexScriptFlattensLegacyDocuments(t *testing.T) {
	requireJQ(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rag_data.json"), []byte(backupData), 0o600); err != nil {
		t.Fatal(err)
	}

	script := GenerateBulkIndexScript("localhost:9200", dir, "rag")
	var batchLine string
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(line, "BATCH_DATA=") {
			batchLine = strings.TrimSpace(line)

			break
		}
	}
	if batchLine == "" {
		t.Fatal("BATCH_DATA assignment not found in script")
	}

	fragment := "START_IDX=0; BATCH_SIZE=1000; INDEX_NAME=rag; DATA_FILE=" + filepath.Join(dir, "rag_data.json") + "\n" +
		batchLine + "\n" + `echo "$BATCH_DATA"`
	lines := strings.Split(runScriptFragment(t, fragment), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 bulk lines, got %d: %v", len(lines), lines)
	}

	if lines[1] != `{"chunk_id":1,"text":"a","doc_id":"x","page_number":2}` {
		t.Errorf("legacy document not flattened: %s", lines[1])
	}
	if lines[3] != `{"chunk_id":2,"text":"b","doc_id":"y"}` {
		t.Errorf("current document must be unchanged: %s", lines[3])
	}
}
