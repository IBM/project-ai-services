package utils

import "math"

// WholeNumber returns v as an int when it holds a whole number, and false otherwise. Numbers read
// back from JSON (e.g. the metadata of a DB row) are float64, while the code that consumes them
// expects an int: a float64 is converted only when it has no fractional part and fits an int, so
// a value such as 32.5 is never silently truncated to 32.
func WholeNumber(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), int64(int(n)) == n
	case float64:
		if n != math.Trunc(n) || n < math.MinInt || n > math.MaxInt {
			return 0, false
		}

		return int(n), true
	default:
		return 0, false
	}
}
