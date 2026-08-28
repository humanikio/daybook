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

// `service install` used to print "installed" over a scheduler that was not
// running, and `service restart` printed "restarted" over a live process whose
// run loop had never started. Both reported the action attempted rather than the
// state reached. serveStarted is the evidence, so reading it has to be right.
func TestServeStartedAt(t *testing.T) {
	cfg := config.Config{Output: config.Output{Root: t.TempDir()}}
	if err := os.MkdirAll(cfg.StateDir(), 0o700); err != nil {
		t.Fatal(err)
	}

	// Nothing recorded yet is not an error, and is not a time.
	if _, ok := serveStartedAt(cfg); ok {
		t.Error("reported a start time with nothing recorded")
	}

	want := time.Now().Truncate(time.Second)
	st := loadLastRun(cfg)
	st.ServeStarted = want.Format(time.RFC3339)
	if err := saveLastRun(cfg, st); err != nil {
		t.Fatal(err)
	}
	got, ok := serveStartedAt(cfg)
	if !ok || !got.Equal(want) {
		t.Fatalf("got %v (%v), want %v", got, ok, want)
	}

	// A stamp that cannot be parsed is "unknown", never a zero time that would
	// read as 1 January year 1 and compare as older than everything.
	st.ServeStarted = "not a timestamp"
	if err := saveLastRun(cfg, st); err != nil {
		t.Fatal(err)
	}
	if _, ok := serveStartedAt(cfg); ok {
		t.Error("an unparseable stamp was reported as a real start time")
	}
}

// The comparison that decides whether a start belongs to THIS command. A stamp
// from a previous run must not count, or a restart that silently did nothing
// reports the last successful start as its own.
func TestOnlyAStartAfterTheCommandCounts(t *testing.T) {
	began := time.Now()
	fresh := began.Add(2 * time.Second)
	stale := began.Add(-10 * time.Minute)

	// One second of slack absorbs clock granularity between writing the stamp at
	// RFC3339 (second precision) and reading it back.
	counts := func(t0 time.Time) bool { return !t0.Before(began.Add(-time.Second)) }

	if !counts(fresh) {
		t.Error("a start after the command was not counted")
	}
	if counts(stale) {
		t.Error("a start from before the command was counted as this one's")
	}
	if !counts(began.Add(-500 * time.Millisecond)) {
		t.Error("sub-second slack was not allowed, so a real start would be missed")
	}
}

// `fmt.Println(action + "ed")` printed "stoped". Every other verb survives the
// concatenation, which is exactly why nobody saw it.
func TestPastTense(t *testing.T) {
	for in, want := range map[string]string{
		"install": "installed", "uninstall": "uninstalled",
		"start": "started", "stop": "stopped", "restart": "restarted",
	} {
		if got := pastTense(in); got != want {
			t.Errorf("pastTense(%q) = %q, want %q", in, got, want)
		}
	}
}
