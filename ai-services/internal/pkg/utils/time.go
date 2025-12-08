package utils

import (
	"fmt"
	"time"
)

// formatTimeDuration formats a duration into a human-readable string
// It returns time elapsed in terms of seconds, minutes, hours, days, weeks, months or years.
func formatTimeDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}

	var result string

	switch {
	case d < time.Minute:
		seconds := d / time.Second
		result = fmt.Sprintf("%d seconds", seconds)
	case d < time.Hour:
		minutes := d / time.Minute
		result = fmt.Sprintf("%d minutes", minutes)
	case d < 24*time.Hour:
		hours := d / time.Hour
		result = fmt.Sprintf("%d hours", hours)
	case d < 7*24*time.Hour:
		days := d / (24 * time.Hour)
		result = fmt.Sprintf("%d days", days)
	case d < 30*24*time.Hour:
		weeks := d / (7 * 24 * time.Hour)
		result = fmt.Sprintf("%d weeks", weeks)
	case d < 365*24*time.Hour:
		months := d / (30 * 24 * time.Hour)
		result = fmt.Sprintf("%d months", months)
	default:
		years := d / (365 * 24 * time.Hour)
		result = fmt.Sprintf("%d years", years)
	}

	return result
}

// TimeAgo formats a time.Time into a human-readable "time ago" string
// For Eg:- "3 hours ago", "2 days ago", etc.
func TimeAgo(t time.Time) string {
	return formatTimeDuration(time.Since(t)) + " ago"
}
