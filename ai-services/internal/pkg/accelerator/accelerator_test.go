package accelerator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountsAdd(t *testing.T) {
	c := Counts{Spyre: 1}
	c.Add(Counts{Spyre: 2, "amd": 3, "none": 0})

	assert.Equal(t, Counts{Spyre: 3, "amd": 3}, c, "counts add up per type and zero counts are ignored")
}

func TestCountsIsEmpty(t *testing.T) {
	assert.True(t, Counts{}.IsEmpty())
	assert.True(t, Counts{Spyre: 0}.IsEmpty())
	assert.False(t, Counts{Spyre: 1}.IsEmpty())
}

func TestCountsCheck(t *testing.T) {
	t.Run("enough of every type", func(t *testing.T) {
		require.NoError(t, Counts{Spyre: 5}.Check(Counts{Spyre: 5, "amd": 2}))
	})

	t.Run("nothing required needs nothing", func(t *testing.T) {
		require.NoError(t, Counts{}.Check(nil))
	})

	t.Run("five required with four available fails", func(t *testing.T) {
		err := Counts{Spyre: 5}.Check(Counts{Spyre: 4})

		var insufficient *InsufficientError
		require.ErrorAs(t, err, &insufficient)
		assert.Equal(t, InsufficientError{Type: Spyre, Need: 5, Have: 4}, *insufficient)
		assert.Contains(t, err.Error(), "required 5, available 4")
	})

	t.Run("a type the worker lacks fails, naming the first short type", func(t *testing.T) {
		err := Counts{Spyre: 1, "amd": 1}.Check(Counts{Spyre: 1})

		var insufficient *InsufficientError
		require.ErrorAs(t, err, &insufficient)
		assert.Equal(t, Type("amd"), insufficient.Type)
		assert.Zero(t, insufficient.Have)
	})
}
