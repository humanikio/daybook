package narrate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/humanikio/daybook/internal/model"
)

// The "what shipped" pass.
//
// The per-stream summaries answer "what was I doing"; this answers "what
// changed, and where do I look" — the difference between a personal record and
// something you can hand to a teammate who was not there.
//
// It is grouped by CAPABILITY, not by commit. Fourteen commits that together
// let you write a SQL transform on an ingest hook are one thing that happened,
// and listing them separately tells a reader nothing they can act on.

const shippedSystem = `You write the "what shipped" section of an engineering daybook. Your reader is
a teammate who was not there and needs to understand what changed and where to
look.

You get every commit from the day — repo, branch, sha, subject, and the files it
touched — plus short summaries of what each stream of work was doing.

Group the commits into CAPABILITIES. One entry per capability, not per commit.
A capability is something a person can now do, or something that now behaves
differently.

Return ONLY a JSON array:
[{"what":"","how":"","where":[""],"commits":[""],"branch":"","internal":false}]

what      One sentence, plain language, present tense, from the point of view of
          whoever uses the thing. "You can now write a SQL transform that runs
          on an ingest hook." NOT "Added executeSQL to the transform
          controller." No jargon a non-engineer would trip on.
how       2-4 sentences for a teammate who has to work on it: what the mechanism
          actually is, and which part matters. Name the important function or
          route if there is one.
where     The files a reader should open, copied EXACTLY from the commits.
          Include backend and frontend when both changed. Three to six paths.
commits   "repo@sha" for every commit in this group.
branch    The branch they landed on.
internal  true when there is no user-facing surface — a refactor, docs, tests,
          tooling. Still describe it; readers skip on the flag, not on silence.

RULES
- COVER EVERYTHING. Every commit must appear in exactly one entry. A day with
  fifty commits does not get five entries.
- Never invent a path, a sha, a repo or a branch. Copy them.
- Order so the most consequential entry is first, internal work last.
- No praise, no grading, no "successfully".`

// Shipped groups a day's commits into capabilities.
func Shipped(ctx context.Context, p Provider, day *model.Day) error {
	facts := shippedFacts(*day)
	if strings.TrimSpace(facts) == "" {
		return nil
	}
	out, err := p.Complete(ctx, shippedSystem, facts)
	if err != nil {
		return err
	}
	raw := extractArray(out)
	if raw == "" {
		return fmt.Errorf("no list in the response")
	}
	if bad := Verify(raw, facts); bad != "" {
		return fmt.Errorf("invented %q", bad)
	}
	var items []model.ShippedItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return err
	}

	// Drop entries with nothing in them rather than rendering empty bullets.
	var kept []model.ShippedItem
	for _, it := range items {
		if strings.TrimSpace(it.What) != "" {
			kept = append(kept, it)
		}
	}
	day.Shipped = kept
	return nil
}

func shippedFacts(d model.Day) string {
	var b strings.Builder
	b.WriteString("COMMITS TODAY:\n\n")
	n := 0
	write := func(c model.Commit) {
		n++
		fmt.Fprintf(&b, "%s@%s [%s] %s\n", c.Repo, c.SHA, c.Branch, c.Subject)
		for i, f := range c.Files {
			if i >= 8 {
				fmt.Fprintf(&b, "    …and %d more files\n", len(c.Files)-8)
				break
			}
			fmt.Fprintf(&b, "    %s\n", f)
		}
	}
	for _, s := range d.Streams {
		for _, c := range s.Commits {
			write(c)
		}
	}
	for _, c := range d.Unattributed {
		write(c)
	}
	if n == 0 {
		return ""
	}

	b.WriteString("\nWHAT THE WORK WAS ABOUT:\n\n")
	for _, s := range d.Streams {
		if s.Narration == nil {
			continue
		}
		fmt.Fprintf(&b, "%s: %s\n", s.Title, s.Narration.Happened)
	}
	return b.String()
}

// ExtractArray is shared with the ledger judge.
func ExtractArray(s string) string { return extractArray(s) }

func extractArray(s string) string {
	start := strings.IndexByte(s, '[')
	end := strings.LastIndexByte(s, ']')
	if start < 0 || end <= start {
		return ""
	}
	return s[start : end+1]
}
