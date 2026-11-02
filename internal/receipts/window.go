package receipts

import (
	"fmt"
	"time"
)

// Windows are the accepted window names and their length.
var Windows = map[string]time.Duration{
	"day": 24 * time.Hour, "week": 7 * 24 * time.Hour, "month": 30 * 24 * time.Hour,
}

// WindowLabel is the window in words, for the share card.
func WindowLabel(name string) string {
	switch name {
	case "day":
		return "last 24 hours"
	case "week":
		return "last 7 days"
	case "month":
		return "last 30 days"
	}
	return name
}

// ParseWindow validates a window name ("" means week).
func ParseWindow(name string) (string, time.Duration, error) {
	if name == "" {
		name = "week"
	}
	d, ok := Windows[name]
	if !ok {
		return "", 0, fmt.Errorf("window must be day, week or month (got %q)", name)
	}
	return name, d, nil
}
