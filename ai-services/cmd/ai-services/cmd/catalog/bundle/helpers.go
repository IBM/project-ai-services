package bundle

import "fmt"

// formatBytes renders a byte count as a human-readable string (KB / MB).
func formatBytes(b int64) string {
	const kb = 1024
	const mb = kb * 1024

	switch {
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/mb)
	case b >= kb:
		return fmt.Sprintf("%.0f KB", float64(b)/kb)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
