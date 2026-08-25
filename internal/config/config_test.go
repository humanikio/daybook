package config

import (
	"testing"
	"time"
)

// "7d" is the first thing anyone types into a field labelled "how far back each
// run looks", and Go's parser rejects it — it stops at hours.
func TestParseDurationAcceptsDaysAndWeeks(t *testing.T) {
	cases := map[string]time.Duration{
		"24h":  24 * time.Hour,
		"7d":   7 * 24 * time.Hour,
		"1d":   24 * time.Hour,
		"2w":   14 * 24 * time.Hour,
		"0.5d": 12 * time.Hour,
		"90m":  90 * time.Minute,
	}
	for in, want := range cases {
		got, err := ParseDuration(in)
		if err != nil {
			t.Errorf("ParseDuration(%q) errored: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseDuration(%q) = %v, want %v", in, got, want)
		}
	}
	for _, bad := range []string{"", "banana", "7 days", "d"} {
		if _, err := ParseDuration(bad); err == nil {
			t.Errorf("ParseDuration(%q) should have failed", bad)
		}
	}
}

// The window is read through Config, so the units have to survive that path too.
func TestWindowLengthUsesTheSameParser(t *testing.T) {
	c := Default()
	c.Window.Length = "7d"
	got, err := c.WindowLength()
	if err != nil || got != 7*24*time.Hour {
		t.Fatalf("WindowLength() = %v, %v — want 168h", got, err)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() rejected a 7d window: %v", err)
	}
}
