package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/humanikio/daybook/internal/config"
)

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

// An upgrade replaces a file, not a process holding that file open. A scheduler
// ran twenty-six hours across four releases reporting itself healthy while
// serving code that no longer existed on disk. verify has to see that.
func TestSchedulerNeedsRestart(t *testing.T) {
	cfg := config.Config{Output: config.Output{Root: t.TempDir()}}
	if err := os.MkdirAll(cfg.StateDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	now := binaryStamp()
	if now == "" {
		t.Skip("cannot stat the test binary here")
	}

	// Started by a version that did not record what it was running — which is
	// the exact case this was added for. Unknown, and said as unknown.
	if got := schedulerNeedsRestart(cfg); !strings.Contains(got, "cannot report what it is running") {
		t.Errorf("with no recorded stamp, got %q", got)
	}

	// Same binary: nothing to report.
	st := loadLastRun(cfg)
	st.ServeStamp = now
	if err := saveLastRun(cfg, st); err != nil {
		t.Fatal(err)
	}
	if got := schedulerNeedsRestart(cfg); got != "" {
		t.Errorf("a scheduler on the current binary was reported stale: %q", got)
	}

	// Replaced binary: the whole point.
	st.ServeStamp = "999-999"
	st.ServeStarted = time.Now().Add(-26 * time.Hour).Format(time.RFC3339)
	if err := saveLastRun(cfg, st); err != nil {
		t.Fatal(err)
	}
	got := schedulerNeedsRestart(cfg)
	if !strings.Contains(got, "older code than the binary on disk") {
		t.Errorf("a replaced binary was not reported: %q", got)
	}
	if !strings.Contains(got, "started") {
		t.Errorf("did not say when it started, which is what makes it believable: %q", got)
	}
}
