package utils

import (
	"fmt"
	"time"
)

const (
	hoursPerDay  = 24
	daysPerWeek  = 7
	daysPerMonth = 30
	daysPerYear  = 365
)

const (
	day   = time.Hour * hoursPerDay
	week  = day * daysPerWeek
	month = day * daysPerMonth
	year  = day * daysPerYear
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
	case d < day:
		hours := d / time.Hour
		result = fmt.Sprintf("%d hours", hours)
	case d < week:
		days := d / day
		result = fmt.Sprintf("%d days", days)
	case d < month:
		weeks := d / week
		result = fmt.Sprintf("%d weeks", weeks)
	case d < year:
		months := d / month
		result = fmt.Sprintf("%d months", months)
	default:
		years := d / year
		result = fmt.Sprintf("%d years", years)
	}

	return result
}

// TimeAgo formats a time.Time into a human-readable "time ago" string
// For Eg:- "3 hours ago", "2 days ago", etc.
func TimeAgo(t time.Time) string {
	return formatTimeDuration(time.Since(t)) + " ago"
}
