package numa

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkNode creates a fake sysfs node directory under root.
// Pass an empty cpulist string to simulate a node with no CPUs.
// Pass memtotalKB == 0 to simulate a node with no memory.
// Both files are written in the exact format the kernel uses.
func mkNode(t *testing.T, root, name, cpulist string, memtotalKB int) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkNode: %v", err)
	}

	// The kernel always writes a trailing newline; match that.
	if err := os.WriteFile(filepath.Join(dir, "cpulist"), []byte(cpulist+"\n"), 0o644); err != nil {
		t.Fatalf("mkNode cpulist: %v", err)
	}

	// Real kernel format: right-aligns the value in an 8-character field.
	memline := fmt.Sprintf("Node 0 MemTotal: %8d kB\n", memtotalKB)
	if err := os.WriteFile(filepath.Join(dir, "meminfo"), []byte(memline), 0o644); err != nil {
		t.Fatalf("mkNode meminfo: %v", err)
	}
}

func newRule(sysfsRoot string) *NumaRule {
	return &NumaRule{sysfsRoot: sysfsRoot}
}

// ---------------------------------------------------------------------------
// parseMemTotalKB unit tests
// ---------------------------------------------------------------------------

func TestParseMemTotalKB(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    uint64
		wantErr bool
	}{
		{
			name:  "real kernel narrow padding",
			input: "Node 0 MemTotal:       0 kB\n",
			want:  0,
		},
		{
			name:  "real kernel wide value",
			input: "Node 4 MemTotal:   506984 kB\n",
			want:  506984,
		},
		{
			name:  "real kernel large value",
			input: "Node 0 MemTotal: 519151680 kB\n",
			want:  519151680,
		},
		{
			name:    "MemTotal token missing",
			input:   "Node 0 MemFree: 1234 kB\n",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "MemTotal: with no following field",
			input:   "Node 0 MemTotal:\n",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseMemTotalKB(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (value=%d)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Verify integration tests
// ---------------------------------------------------------------------------

// TestEmptyPlaceholderNodesIgnored verifies that empty firmware-topology nodes
// (no CPUs, no memory) are not counted as active, so a single populated node
// surrounded by empty neighbours is correctly treated as aligned.
func TestEmptyPlaceholderNodesIgnored(t *testing.T) {
	root := t.TempDir()
	mkNode(t, root, "node0", "", 0)           // empty placeholder
	mkNode(t, root, "node1", "0-119", 519151) // all CPUs and memory
	mkNode(t, root, "node2", "", 0)           // empty placeholder

	if err := newRule(root).Verify(context.Background()); err != nil {
		t.Errorf("empty placeholders ignored: expected pass, got: %v", err)
	}
}

// TestSinglePopulatedNode — trivial case: one node with CPUs. Must PASS.
func TestSinglePopulatedNode(t *testing.T) {
	root := t.TempDir()
	mkNode(t, root, "node0", "0-15", 524288)

	if err := newRule(root).Verify(context.Background()); err != nil {
		t.Errorf("single node: expected pass, got: %v", err)
	}
}

// TestTwoNodesWithCPUs — CPUs split across two nodes. Must FAIL.
func TestTwoNodesWithCPUs(t *testing.T) {
	root := t.TempDir()
	mkNode(t, root, "node0", "0-7", 262144)
	mkNode(t, root, "node1", "8-15", 262144)

	err := newRule(root).Verify(context.Background())
	if err == nil {
		t.Error("two active nodes: expected failure, got nil")
	}
}

// TestCPUsOnOneNodeMemoryOnAnother — CPUs on node0, memory only on node1.
// Both count as active (cross-node penalty exists). Must FAIL.
func TestCPUsOnOneNodeMemoryOnAnother(t *testing.T) {
	root := t.TempDir()
	mkNode(t, root, "node0", "0-15", 0)  // CPUs, no memory
	mkNode(t, root, "node1", "", 524288) // memory, no CPUs

	err := newRule(root).Verify(context.Background())
	if err == nil {
		t.Error("CPUs and memory on different nodes: expected failure, got nil")
	}
}

// TestNoNodesFound — sysfs root exists but contains no node* dirs. Must FAIL.
func TestNoNodesFound(t *testing.T) {
	root := t.TempDir()

	err := newRule(root).Verify(context.Background())
	if err == nil {
		t.Error("no nodes: expected failure, got nil")
	}
}

// TestSysfsRootMissing — sysfs root path does not exist. Must FAIL.
func TestSysfsRootMissing(t *testing.T) {
	err := newRule("/nonexistent/sysfs/path").Verify(context.Background())
	if err == nil {
		t.Error("missing sysfs root: expected failure, got nil")
	}
}

// TestAllEmptyNodes — all node dirs exist but every one is empty (no CPUs, no memory).
// Must FAIL with "no active NUMA nodes".
func TestAllEmptyNodes(t *testing.T) {
	root := t.TempDir()
	mkNode(t, root, "node0", "", 0)
	mkNode(t, root, "node1", "", 0)

	err := newRule(root).Verify(context.Background())
	if err == nil {
		t.Error("all empty nodes: expected failure, got nil")
	}
}

// TestMissingCpulist — node directory exists but cpulist file is absent.
// Verify must propagate the error rather than silently treating the node as inactive.
func TestMissingCpulist(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "node0")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// deliberately omit cpulist; write only meminfo
	if err := os.WriteFile(filepath.Join(dir, "meminfo"), fmt.Appendf(nil, "Node 0 MemTotal: %8d kB\n", 0), 0o644); err != nil {
		t.Fatal(err)
	}

	err := newRule(root).Verify(context.Background())
	if err == nil {
		t.Error("missing cpulist: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "cpulist") {
		t.Errorf("expected error to mention cpulist, got: %v", err)
	}
}
