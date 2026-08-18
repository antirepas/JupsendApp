package model

import (
	"fmt"
	"time"
)

// FormatWaitRemaining returns a short remaining duration and absolute wake time for UI.
// remaining examples: "due now", "45m", "3h 20m", "2d 4h"
func FormatWaitRemaining(wake *time.Time, status string) (remaining, absolute string) {
	if status != "waiting" || wake == nil {
		return "", ""
	}
	absolute = wake.Local().Format("Jan 2, 3:04 PM")
	d := time.Until(*wake)
	if d <= 0 {
		return "due now", absolute
	}
	return formatDurationShort(d), absolute
}

func formatDurationShort(d time.Duration) string {
	if d < time.Minute {
		return "<1m"
	}
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	mins := int(d / time.Minute)
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case days > 0:
		return fmt.Sprintf("%dd", days)
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
