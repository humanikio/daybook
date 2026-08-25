package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/humanikio/daybook/internal/model"
	"github.com/humanikio/daybook/internal/narrate"
)

const judgeSystem = `You decide whether items in an engineering ledger were finished today.

Each item is something the person already did that had not finished proving
itself — shipped but untested, blocked, unverified. You are given today's
commits and short summaries of today's work.

Return ONLY a JSON array:
[{"id":"","closed":true,"evidence":{"kind":"commit","repo":"","sha":"","quote":""}}]

kind is "commit" (cite repo and sha) or "session" (cite a short quote from a
summary, copied exactly).

RULES
- DEFAULT TO NOT CLOSING. Only close an item when today's work demonstrably
  finished it. Uncertain means still open.
- Every close MUST carry evidence naming a sha or quoting a summary verbatim.
  A close you cannot evidence will be rejected and the item left open.
- Never invent a sha, repo or quote.
- Include only the items you are closing. Omit the rest.`

// Judge asks a provider which candidates today's work closed, then refuses any
// close it cannot verify.
//
// Same three-stage shape as commit attribution: narrow deterministically, judge
// the remainder, then gate the judgement on evidence that must exist in the
// input. The gate is what makes a model safe to put here at all.
func Judge(ctx context.Context, p narrate.Provider, items []model.OpenItem, day model.Day, on time.Time) ([]model.OpenItem, []model.OpenItem) {
	cands := Candidates(items, day)
	if len(cands) == 0 {
		return items, nil
	}

	facts := judgeFacts(cands, day)
	out, err := p.Complete(ctx, judgeSystem, facts)
	if err != nil {
		return items, nil
	}
	raw := extractArray(out)
	if raw == "" {
		return items, nil
	}

	var verdicts []struct {
		ID       string         `json:"id"`
		Closed   bool           `json:"closed"`
		Evidence model.Evidence `json:"evidence"`
	}
	if json.Unmarshal([]byte(raw), &verdicts) != nil {
		return items, nil
	}

	// Every sha the day actually contains. A close naming anything else is a
	// fabrication, whatever it claims.
	shas := map[string]bool{}
	for _, s := range day.Streams {
		for _, c := range s.Commits {
			shas[strings.ToLower(c.SHA)] = true
		}
	}
	for _, c := range day.Unattributed {
		shas[strings.ToLower(c.SHA)] = true
	}

	var closed []model.OpenItem
	for _, v := range verdicts {
		if !v.Closed {
			continue
		}
		switch v.Evidence.Kind {
		case "commit":
			if v.Evidence.SHA == "" || !shas[strings.ToLower(v.Evidence.SHA)] {
				continue // cites a commit that does not exist today
			}
		case "session":
			q := strings.TrimSpace(v.Evidence.Quote)
			if len(q) < 12 || !strings.Contains(facts, q) {
				continue // cites words nobody wrote
			}
		default:
			continue // no evidence kind at all
		}
		var ok bool
		if items, ok = Close(items, v.ID, v.Evidence, on); ok {
			for _, it := range items {
				if it.ID == v.ID {
					closed = append(closed, it)
				}
			}
		}
	}
	return items, closed
}

func judgeFacts(cands []model.OpenItem, day model.Day) string {
	var b strings.Builder
	b.WriteString("OPEN ITEMS UP FOR REVIEW:\n")
	for _, it := range cands {
		fmt.Fprintf(&b, "- id=%s  opened %s  (%s)  %s\n", it.ID, it.Opened, it.Stream, it.Text)
	}
	b.WriteString("\nTODAY'S COMMITS:\n")
	for _, s := range day.Streams {
		for _, c := range s.Commits {
			fmt.Fprintf(&b, "- %s@%s %s\n", c.Repo, c.SHA, c.Subject)
		}
	}
	for _, c := range day.Unattributed {
		fmt.Fprintf(&b, "- %s@%s %s\n", c.Repo, c.SHA, c.Subject)
	}
	b.WriteString("\nTODAY'S WORK:\n")
	for _, s := range day.Streams {
		if s.Narration == nil {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n", s.Title, s.Narration.Happened)
	}
	return b.String()
}

func extractArray(s string) string { return narrate.ExtractArray(s) }
