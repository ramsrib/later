package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := map[string]time.Duration{
		"30m": 30 * time.Minute,
		"4h":  4 * time.Hour,
		"3d":  72 * time.Hour,
		"2w":  14 * 24 * time.Hour,
		// 0m means "due now" — the natural way to say "surface this next session".
		"0m": 0,
	}
	for input, want := range tests {
		t.Run(input, func(t *testing.T) {
			got, err := parseDuration(input)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("parseDuration(%q) = %v, want %v", input, got, want)
			}
		})
	}
}

func TestParseDurationRejectsUnsupportedAndMalformedValues(t *testing.T) {
	for _, input := range []string{"", "m", "-1h", "1.5h", "1s", "2mo", "1y"} {
		t.Run(input, func(t *testing.T) {
			_, err := parseDuration(input)
			if err == nil {
				t.Fatalf("parseDuration(%q) unexpectedly succeeded", input)
			}
			if !strings.Contains(err.Error(), "m, h, d, or w") {
				t.Fatalf("error %q does not name supported units", err)
			}
		})
	}
}
