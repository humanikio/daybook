package wizard

import "testing"

// One address routinely appears under several names — a handle in one repo, a
// full name in another. Without folding on the email the suggested filter held
// the same address three times, and a one-person machine looked like a team.
func TestEmailsOfDeduplicates(t *testing.T) {
	in := []string{
		"a-handle <someone@example.com>",
		"Someone Person <someone@example.com>",
		"Someone Person <SOMEONE@example.com>",
		"Other <other@example.org>",
		"bare@example.net",
	}
	got := emailsOf(in)
	want := []string{"someone@example.com", "other@example.org", "bare@example.net"}
	if len(got) != len(want) {
		t.Fatalf("emailsOf() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("emailsOf()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// The typo suggester only ever suggests, so it has to be right when it speaks.
// One adjacent transposition or a case difference; nothing looser.
func TestCloseEnough(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"destkop", "desktop", true},
		{"documnets", "documents", true},
		{"desktop", "desktop", false},   // identical is not a transposition
		{"desktop", "documents", false}, // different lengths
		{"abcd", "badc", false},         // two separate swaps, not one
		{"projects", "prpjects", false}, // a substitution, not a transposition
	}
	for _, c := range cases {
		if got := closeEnough(c.a, c.b); got != c.want {
			t.Errorf("closeEnough(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// splitOutput backs the "create it in / folder name" prompts, and re-running
// setup must offer back what was chosen last time.
func TestSplitOutputRoundTrips(t *testing.T) {
	cases := map[string][2]string{
		"~/Desktop/daybook":   {"~/Desktop", "daybook"},
		"~/Documents/worklog": {"~/Documents", "worklog"},
		"daybook":             {"~/Desktop", "daybook"},
		"":                    {"~/Desktop", "daybook"},
	}
	for in, want := range cases {
		p, n := splitOutput(in)
		if p != want[0] || n != want[1] {
			t.Errorf("splitOutput(%q) = (%q, %q), want (%q, %q)", in, p, n, want[0], want[1])
		}
	}
}
