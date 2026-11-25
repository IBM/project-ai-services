package podman

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHostPathFlag(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "present")
	assert.NoError(t, os.Mkdir(existing, 0o755))

	assert.Equal(t, "true", hostPathFlag(existing))
	assert.Equal(t, "false", hostPathFlag(filepath.Join(existing, "missing")))
}
