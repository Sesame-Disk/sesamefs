package httputil

import (
	"fmt"
	"time"
)

// FormatSizeSeafile formats bytes in Seafile's format with non-breaking space.
// Examples: "0\u00a0bytes", "1.5\u00a0KB"
func FormatSizeSeafile(bytes int64) string {
	const nbsp = "\u00a0" // Non-breaking space (U+00A0)
	if bytes == 0 {
		return "0" + nbsp + "bytes"
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d"+nbsp+"bytes", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f"+nbsp+"%cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FormatRelativeTimeHTML formats time as Seafile's HTML <time> tag.
// Example: <time datetime="2026-01-16T02:00:27" is="relative-time" title="...">4 seconds ago</time>
func FormatRelativeTimeHTML(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	var relativeStr string
	if diff < time.Minute {
		seconds := int(diff.Seconds())
		if seconds <= 1 {
			relativeStr = "1 second ago"
		} else {
			relativeStr = fmt.Sprintf("%d seconds ago", seconds)
		}
	} else if diff < time.Hour {
		minutes := int(diff.Minutes())
		if minutes == 1 {
			relativeStr = "1 minute ago"
		} else {
			relativeStr = fmt.Sprintf("%d minutes ago", minutes)
		}
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			relativeStr = "1 hour ago"
		} else {
			relativeStr = fmt.Sprintf("%d hours ago", hours)
		}
	} else {
		days := int(diff.Hours() / 24)
		if days == 1 {
			relativeStr = "1 day ago"
		} else {
			relativeStr = fmt.Sprintf("%d days ago", days)
		}
	}

	datetime := t.UTC().Format("2006-01-02T15:04:05")
	title := t.UTC().Format("Mon, 02 Jan 2006 15:04:05 -0700")
	return fmt.Sprintf("<time datetime=\"%s\" is=\"relative-time\" title=\"%s\" >%s</time>",
		datetime, title, relativeStr)
}
