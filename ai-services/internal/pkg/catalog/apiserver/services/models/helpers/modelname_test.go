package helpers

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestComponentName(t *testing.T) {
	id := uuid.MustParse("1a2b3c4d-0000-0000-0000-000000000000")

	assert.Equal(t, "my-app-1a2b3c4d", ComponentName("my-app", id))
}

func TestComponentName_FitsColumn(t *testing.T) {
	name := ComponentName(strings.Repeat("a", 100), uuid.New())

	assert.Len(t, name, maxComponentNameLen)
}

func TestComponentName_DiffersPerID(t *testing.T) {
	assert.NotEqual(t, ComponentName("my-app", uuid.New()), ComponentName("my-app", uuid.New()))
}
