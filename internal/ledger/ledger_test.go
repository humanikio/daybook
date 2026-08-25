package ledger

import (
	"testing"
	"time"

	"github.com/humanikio/daybook/internal/model"
)

func day(date string, streamID string, open ...string) model.Day {
	t, _ := time.Parse("2006-01-02", date)
	return model.Day{
		WindowEnd: t,
		Streams: []model.Stream{{
			ID: streamID, Title: "A stream",
			Narration: &model.Narration{Happened: "x", Open: open},
		}},
	}
}

// Re-narrating a day must not double its contribution. A model rephrases the
// same concern between runs, so identity-by-text-hash alone let one day go from
// 36 items to 110.
func TestRenarratingADayIsIdempotent(t *testing.T) {
	items := Merge(nil, day("2026-08-25", "s1", "untested in prod", "needs a deploy"))
	if got := len(Open(items)); got != 2 {
		t.Fatalf("first narration produced %d items, want 2", got)
	}
	// Same day, same stream, the model words it differently.
	items = Merge(items, day("2026-08-25", "s1", "has not been tested in prod", "still needs deploying"))
	if got := len(Open(items)); got != 2 {
		t.Fatalf("re-narration produced %d items, want 2", got)
	}
}

// A different day adds to the ledger rather than replacing it — that is the
// whole point of it being a ledger.
func TestAnotherDayAccumulates(t *testing.T) {
	items := Merge(nil, day("2026-08-25", "s1", "untested in prod"))
	items = Merge(items, day("2026-08-26", "s1", "a new concern"))
	if got := len(Open(items)); got != 2 {
		t.Fatalf("got %d items across two days, want 2", got)
	}
}

// Closing is a decision. Re-narrating the day it came from must not resurrect it.
func TestClosedItemsAreNotResurrected(t *testing.T) {
	items := Merge(nil, day("2026-08-25", "s1", "untested in prod"))
	id := Open(items)[0].ID
	items, ok := Close(items, id, model.Evidence{Kind: "manual"}, time.Now())
	if !ok {
		t.Fatal("close failed")
	}
	items = Merge(items, day("2026-08-25", "s1", "untested in prod"))
	if got := len(Open(items)); got != 0 {
		t.Fatalf("a closed item came back: %d open", got)
	}
}
