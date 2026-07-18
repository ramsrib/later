package main

import (
	"fmt"
	"strconv"
	"time"
)

func parseDuration(value string) (time.Duration, error) {
	if len(value) < 2 {
		return 0, durationError(value)
	}
	unit := value[len(value)-1]
	n, err := strconv.ParseInt(value[:len(value)-1], 10, 64)
	if err != nil || n < 0 {
		return 0, durationError(value)
	}
	var base time.Duration
	switch unit {
	case 'm':
		base = time.Minute
	case 'h':
		base = time.Hour
	case 'd':
		base = 24 * time.Hour
	case 'w':
		base = 7 * 24 * time.Hour
	default:
		return 0, durationError(value)
	}
	max := int64(^uint64(0) >> 1)
	if n > max/int64(base) {
		return 0, fmt.Errorf("duration %q is too large", value)
	}
	return time.Duration(n) * base, nil
}

func durationError(value string) error {
	return fmt.Errorf("invalid duration %q: use an integer followed by one supported unit: m, h, d, or w (months and years are not supported)", value)
}

func parseWhen(value string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04", value, now.Location()); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q: use RFC3339 or YYYY-MM-DD HH:MM (local time)", value)
}
