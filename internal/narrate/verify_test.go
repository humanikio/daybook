package narrate

import "testing"

// The gate reported zero rejections on its first real run. That is the right
// outcome and also indistinguishable from a gate that never fires, so it is
// pinned here: a check nobody has seen reject anything is not a check.
func TestVerifyRejectsFabrications(t *testing.T) {
	facts := `COMMITS ATTRIBUTED TO THIS STREAM:
- api@7eb0f964 Fix two defects found by running the flow end to end (+40/-6)
- web@a1b2c3d4 Render the SQL Nova proposes (+355/-1)
FILES: internal/vcs/vcs.go`

	cases := []struct {
		name, out string
		wantBad   string
	}{
		{"clean", `{"happened":"Fixed the signer in api@7eb0f964 and touched internal/vcs/vcs.go"}`, ""},
		{"invented sha", `{"happened":"Closed by api@deadbee1"}`, "deadbee1"},
		{"invented path", `{"happened":"Rewrote internal/nope/absent.go"}`, "internal/nope/absent.go"},
		{"real sha, different case", `{"happened":"see 7EB0F964"}`, ""},
		{"digits are not shas", `{"happened":"took 12345678 milliseconds"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Verify(c.out, facts); got != c.wantBad {
				t.Fatalf("Verify() = %q, want %q", got, c.wantBad)
			}
		})
	}
}

func TestExtractJSONSurvivesWrapping(t *testing.T) {
	// Models fence and preamble their JSON however firmly you ask them not to.
	for _, in := range []string{
		`{"a":1}`,
		"Here you go:\n```json\n{\"a\":1}\n```",
		"```\n{\"a\":1}\n```\nHope that helps.",
	} {
		if got := extractJSON(in); got != `{"a":1}` {
			t.Fatalf("extractJSON(%q) = %q", in, got)
		}
	}
	if got := extractJSON("no json here"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}
