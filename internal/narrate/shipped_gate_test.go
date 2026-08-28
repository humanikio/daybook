package narrate

import (
	"strings"
	"testing"

	"github.com/humanikio/daybook/internal/model"
)

// One bad entry must not take the good ones with it. A day's whole capability
// list — the section a teammate actually reads — was discarded because a single
// token in a single entry did not match.
func TestOneBadEntryDoesNotDiscardTheRest(t *testing.T) {
	facts := "COMMITS TODAY:\n\nweb@abc1234 [main] real work\n    src/real/File.tsx\n"

	good := model.ShippedItem{
		What: "You can now do the thing", How: "It works.",
		Where: []string{"src/real/File.tsx"}, Commits: []string{"web@abc1234"}, Branch: "main",
	}
	bad := model.ShippedItem{
		What: "You can now do another thing", How: "It also works.",
		Where: []string{"src/made/Up.tsx"}, Commits: []string{"web@abc1234"}, Branch: "main",
	}

	if got := Verify(itemTokens(good), facts); got != "" {
		t.Fatalf("a legitimate entry was rejected over %q", got)
	}
	if got := Verify(itemTokens(bad), facts); got != "src/made/Up.tsx" {
		t.Fatalf("the fabricated path was not caught, got %q", got)
	}
}

// The prose is checked too: a sha named in a sentence is as checkable as one in
// a list, and is exactly where a plausible-looking fabrication would hide.
func TestProseIsVerifiedNotJustTheFields(t *testing.T) {
	facts := "web@abc1234 [main] real work\n    src/real/File.tsx\n"
	it := model.ShippedItem{
		What:    "You can now do the thing",
		How:     "See web@deadbee for the follow-up.",
		Commits: []string{"web@abc1234"}, Branch: "main",
	}
	if got := Verify(itemTokens(it), facts); !strings.Contains(got, "deadbee") {
		t.Errorf("a sha invented in prose was not caught, got %q", got)
	}
}
