package preview

import "testing"

func TestClosestNeedsRealOverlap(t *testing.T) {
	opts := []string{
		"You can now test an ingest transform against payloads the source actually received",
		"A drip now holds its interval no matter which editor saved it",
	}
	if got := closest("Test an ingest transform against payloads the source received", opts); got != opts[0] {
		t.Fatalf("paraphrase should resolve to the original, got %q", got)
	}
	if got := closest("something else entirely about billing", opts); got != "" {
		t.Fatalf("unrelated claim must resolve to nothing, got %q", got)
	}
	if got := closest("the page", opts); got != "" {
		t.Fatalf("too vague to key on, got %q", got)
	}
}
