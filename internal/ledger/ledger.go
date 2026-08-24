// Package ledger keeps the running list of work that has not finished proving
// itself.
//
// An OPEN item is not a todo. It is something you already did that is still
// unproven: shipped but untested, blocked on someone, unverified in prod. That
// distinction is the whole value — a todo list is aspirational, this is a
// record of debt you have already taken on.
//
// The list is a LEDGER, not a nightly regeneration. Regenerating would silently
// drop anything raised on Monday and untouched on Tuesday, and those are exactly
// the items that rot. Nothing here is ever deleted; items change status.
package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tndigitalmark/claude-code-daybook/internal/config"
	"github.com/tndigitalmark/claude-code-daybook/internal/model"
)

func path(cfg config.Config) string { return filepath.Join(cfg.StateDir(), "open.json") }

// Load reads the ledger. A missing file is an empty ledger, not an error.
func Load(cfg config.Config) []model.OpenItem {
	b, err := os.ReadFile(path(cfg))
	if err != nil {
		return nil
	}
	var items []model.OpenItem
	if json.Unmarshal(b, &items) != nil {
		return nil
	}
	return items
}

func Save(cfg config.Config, items []model.OpenItem) error {
	if err := os.MkdirAll(cfg.StateDir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path(cfg), b, 0o600)
}

var norm = regexp.MustCompile(`[^a-z0-9]+`)

// key identifies an item by its stream and the shape of its text, so the same
// concern raised on three consecutive days is one item that ages rather than
// three that look like a growing problem.
func key(streamID, text string) string {
	n := norm.ReplaceAllString(strings.ToLower(text), " ")
	sum := sha256.Sum256([]byte(streamID + "|" + strings.TrimSpace(n)))
	return hex.EncodeToString(sum[:])[:8]
}

// Merge folds a day's narrated open items into the ledger.
func Merge(items []model.OpenItem, day model.Day) []model.OpenItem {
	byID := map[string]int{}
	for i, it := range items {
		byID[it.ID] = i
	}
	date := day.WindowEnd.Format("2006-01-02")

	for _, s := range day.Streams {
		if s.Narration == nil {
			continue
		}
		var repos []string
		for r := range s.Repos {
			repos = append(repos, r)
		}
		sort.Strings(repos)

		for _, text := range s.Narration.Open {
			text = strings.TrimSpace(text)
			if text == "" {
				continue
			}
			id := key(s.ID, text)
			if i, ok := byID[id]; ok {
				// Seen again today. Closed items are NOT reopened here — only a
				// person reopens something the ledger already settled.
				if items[i].Status == "open" {
					items[i].LastSeen = date
					items[i].Repos = repos
				}
				continue
			}
			items = append(items, model.OpenItem{
				ID: id, Text: text, Stream: s.Title, StreamID: s.ID,
				Repos: repos, Opened: date, LastSeen: date, Status: "open",
			})
			byID[id] = len(items) - 1
		}
	}
	return items
}

// Candidates narrows the ledger to items today's work could plausibly have
// closed.
//
// This is the cheap deterministic pass that runs BEFORE any model sees
// anything: forty open items against fifty commits becomes a handful. The rest
// stay open for free, which is both correct and the reason this scales.
func Candidates(items []model.OpenItem, day model.Day) []model.OpenItem {
	touched := map[string]bool{}
	active := map[string]bool{}
	for _, s := range day.Streams {
		active[s.ID] = true
		for _, c := range s.Commits {
			touched[c.Repo] = true
		}
	}
	for _, c := range day.Unattributed {
		touched[c.Repo] = true
	}

	var out []model.OpenItem
	for _, it := range items {
		if it.Status != "open" {
			continue
		}
		if active[it.StreamID] {
			out = append(out, it)
			continue
		}
		for _, r := range it.Repos {
			if touched[r] {
				out = append(out, it)
				break
			}
		}
	}
	return out
}

// Close marks one item closed with evidence.
//
// Evidence is REQUIRED. A close that cannot point at what closed it is refused,
// because a ledger that empties itself on a hunch is worse than no ledger.
func Close(items []model.OpenItem, id string, ev model.Evidence, on time.Time) ([]model.OpenItem, bool) {
	for i := range items {
		if items[i].ID != id || items[i].Status != "open" {
			continue
		}
		items[i].Status = "closed"
		items[i].Closed = on.Format("2006-01-02")
		items[i].ClosedBy = &ev
		return items, true
	}
	return items, false
}

// Reopen undoes a close. Closure gets things wrong in both directions and false
// positives are the dangerous one, so being wrong must cost one command.
func Reopen(items []model.OpenItem, id string) ([]model.OpenItem, bool) {
	for i := range items {
		if items[i].ID == id && items[i].Status == "closed" {
			items[i].Status = "open"
			items[i].Closed = ""
			items[i].ClosedBy = nil
			return items, true
		}
	}
	return items, false
}

// Open returns the still-open items, oldest first — age is the honest signal.
// An item open for thirty days is either done and unrecorded, or genuinely
// rotting, and either way it is the one worth looking at.
func Open(items []model.OpenItem) []model.OpenItem {
	var out []model.OpenItem
	for _, it := range items {
		if it.Status == "open" {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Opened < out[j].Opened })
	return out
}
