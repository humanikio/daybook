package tui

import "testing"

// A row wider than the terminal wraps, and the redraw walks back a fixed
// number of lines — so one wrapped row leaves a copy of the header on screen
// for every keypress. Two long emails in one value is the case that hit.
func TestFitKeepsBothEnds(t *testing.T) {
	long := "first.person@example.com, 173967893+someone@users.noreply.github.com"
	got := fit(long, 40)
	// Columns, not bytes: the ellipsis is one column and three bytes, and
	// measuring the wrong one is what made this overshoot in the first place.
	if n := len([]rune(got)); n > 40 {
		t.Fatalf("fit produced %d columns, want <= 40: %q", n, got)
	}
	// Middle-elision, because the ends carry the meaning: lopping the tail off
	// two long addresses leaves rows that look identical.
	if got[:6] != "first." {
		t.Errorf("lost the head: %q", got)
	}
	if r := []rune(got); string(r[len(r)-6:]) != "ub.com" {
		t.Errorf("lost the tail: %q", got)
	}
}

func TestFitLeavesShortValuesAlone(t *testing.T) {
	for _, s := range []string{"24h", "yes", "on · Claude Code", ""} {
		if got := fit(s, 40); got != s {
			t.Errorf("fit(%q) = %q, want unchanged", s, got)
		}
	}
}
