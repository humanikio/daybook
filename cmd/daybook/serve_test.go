package main

import "testing"

// The scheduler exits when its own binary changes, so launchd restarts it on the
// new one. The risk in that is a false positive: a stamp that cannot be read
// must not look like a replaced binary, or the service exits every minute and
// the restart loop is the bug rather than the fix.
func TestBinaryStampIsStableAcrossCalls(t *testing.T) {
	a := binaryStamp()
	if a == "" {
		t.Skip("cannot stat the test binary here")
	}
	if b := binaryStamp(); b != a {
		t.Fatalf("two calls disagreed (%q then %q) — the scheduler would exit every minute", a, b)
	}
}

// An empty stamp means "could not tell", and could-not-tell is not evidence of
// a change. This mirrors the guard in serveLoop.
func TestEmptyStampNeverCountsAsAChange(t *testing.T) {
	changed := func(self, now string) bool {
		return now != "" && self != "" && now != self
	}
	for _, c := range []struct {
		self, now string
		want      bool
	}{
		{"1-2", "1-2", false}, // same binary
		{"1-2", "9-9", true},  // replaced
		{"", "9-9", false},    // could not read at start
		{"1-2", "", false},    // could not read now
		{"", "", false},       // could not read either time
	} {
		if got := changed(c.self, c.now); got != c.want {
			t.Errorf("self=%q now=%q → %v, want %v", c.self, c.now, got, c.want)
		}
	}
}
