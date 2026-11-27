package utils

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWholeNumber(t *testing.T) {
	tests := []struct {
		name   string
		in     any
		want   int
		wantOK bool
	}{
		{"int", 64, 64, true},
		{"int64", int64(64), 64, true},
		{"whole float64, as read back from JSON", float64(131072), 131072, true},
		{"negative whole float64", float64(-3), -3, true},
		{"fractional float64 is not truncated", 32.5, 0, false},
		{"NaN", math.NaN(), 0, false},
		{"infinity", math.Inf(1), 0, false},
		{"float64 beyond the int range", math.MaxFloat64, 0, false},
		{"string", "64", 0, false},
		{"nil", nil, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := WholeNumber(tt.in)

			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
