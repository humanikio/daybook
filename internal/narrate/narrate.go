package narrate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/humanikio/daybook/internal/config"
	"github.com/humanikio/daybook/internal/model"
)

// Result reports what narration did, for the caller to print honestly.
type Result struct {
	Provider string
	Streams  int
	Rejected int // failed the verification gate
	Failed   int // provider error

	// Fabrications records what each rejection actually claimed.
	//
	// "1 rejected by the verification gate" is not enough to act on: it does
	// not say whether the model invented a commit or whether the gate is
	// wrong. Both are worth knowing and they call for opposite fixes.
	Fabrications []string
}

const streamSystem = `You write one entry in a private engineering daybook: a factual record of what
somebody did today, for that person to read tomorrow.

You are given the derived facts for ONE stream of work — the prompts the person
typed, what the assistant said back, and the commits attributed to it.

Return ONLY a JSON object:
{"intent":"","happened":"","decisions":[],"open":[],"carryForward":""}

intent        One sentence. What they were trying to do.
happened      2-4 sentences. What was actually done, found, built or broken.
              Most of this comes from the assistant's messages, not the prompts.
decisions     Choices made that no commit records. Omit if none.
open          Work already done that has not finished proving itself:
              shipped-but-untested, blocked, unverified in prod. Not a todo list.
carryForward  One line. Where this stream stands now.

RULES
- Never restate the numbers. Times, prompt counts, commit counts, shas and
  line counts are already printed directly above your text. Repeating them is
  the single most common failure here.
- Never invent a sha, a filename or a path. If it is not in the facts given to
  you, it does not exist.
- No praise, no grading, no encouragement. "92 tool calls and nothing recorded
  as produced" is a fact and belongs in happened; "unproductive" is a judgement
  and does not.
- No speculation. If the record does not say why something stopped, say it
  stopped.
- Plain past tense. No headings, no markdown, no bullets inside a field.`

const daySystem = `You write the opening paragraph of a private engineering daybook.

You are given short summaries of each stream of work from one day. You have NOT
seen the underlying detail and must not pretend otherwise.

Return ONLY a JSON object:
{"shape":"","moved":"","carrying":""}

shape     2-3 sentences on how the day actually went. This is the one place a
          cross-stream connection can be drawn - say so when several streams
          were really the same piece of work.
moved     What genuinely advanced.
carrying  What is live going into tomorrow.

RULES
- Never restate counts, times or shas.
- Never invent anything not in the summaries.
- No praise, no grading. Record, not report card.`

// Run narrates a day in place.
//
// Per stream, not per day. Twelve small calls rather than one enormous one:
// each fits comfortably, they run concurrently, one failure loses one stream
// instead of the day, and the largest single call drops from the whole day's
// text to a single stream's.
func Run(ctx context.Context, cfg config.Config, day *model.Day, carry map[string]string) (Result, error) {
	p, err := Resolve(cfg)
	if err != nil {
		return Result{}, err
	}
	res := Result{Provider: p.Name()}

	conc := cfg.Narrate.Concurrency
	if conc <= 0 {
		conc = 3
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := range day.Streams {
		if day.Streams[i].Agent {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			facts := streamFacts(day.Streams[i], carry[day.Streams[i].ID])
			out, err := p.Complete(ctx, streamSystem, facts)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				res.Failed++
				return
			}
			raw := extractJSON(out)
			if raw == "" {
				res.Rejected++
				return
			}
			// The gate runs on the RAW model output, before parsing, so a
			// fabricated identifier is caught wherever in the response it landed.
			if bad := Verify(raw, facts); bad != "" {
				res.Rejected++
				res.Fabrications = append(res.Fabrications,
					fmt.Sprintf("%s: claimed %q", day.Streams[i].Title, bad))
				return
			}
			var n model.Narration
			if err := json.Unmarshal([]byte(raw), &n); err != nil {
				res.Rejected++
				return
			}
			if strings.TrimSpace(n.Happened) == "" {
				res.Rejected++
				return
			}
			day.Streams[i].Narration = &n
			res.Streams++
		}(i)
	}
	wg.Wait()

	if res.Streams == 0 {
		return res, nil
	}

	// Synthesis runs over the per-stream summaries only — never the transcripts.
	sum := daySummaries(*day)
	if out, err := p.Complete(ctx, daySystem, sum); err == nil {
		if raw := extractJSON(out); raw != "" && Verify(raw, sum) == "" {
			var dn model.DayNarration
			if json.Unmarshal([]byte(raw), &dn) == nil && strings.TrimSpace(dn.Shape) != "" {
				day.Narration = &dn
			}
		}
	}
	return res, nil
}

// streamFacts is everything the model is allowed to see about one stream.
func streamFacts(s model.Stream, carry string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "STREAM: %s\n", s.Title)
	fmt.Fprintf(&b, "WHEN: %s to %s\n", s.First.Format("Mon 15:04"), s.Last.Format("Mon 15:04"))
	if len(s.Repos) > 0 {
		var rs []string
		for r := range s.Repos {
			rs = append(rs, r)
		}
		fmt.Fprintf(&b, "REPOS: %s\n", strings.Join(rs, ", "))
	}
	if carry != "" {
		fmt.Fprintf(&b, "PREVIOUSLY: %s\n", carry)
	}

	b.WriteString("\nWHAT THEY ASKED FOR:\n")
	for _, p := range s.Prompts {
		fmt.Fprintf(&b, "- [%s] %s\n", p.At.Format("15:04"), clip(p.Text, 600))
	}

	// The assistant's side is most of the substance — what was found, built or
	// broken lives here rather than in the prompts.
	if len(s.Notes) > 0 {
		b.WriteString("\nWHAT THE ASSISTANT REPORTED:\n")
		for _, n := range lastN(s.Notes, 60) {
			fmt.Fprintf(&b, "- [%s] %s\n", n.At.Format("15:04"), clip(n.Text, 900))
		}
	}

	if len(s.Failed) > 0 {
		b.WriteString("\nCOMMANDS THAT FAILED:\n")
		for _, f := range s.Failed {
			fmt.Fprintf(&b, "- %s\n", clip(f, 300))
		}
	}

	if len(s.Commits) > 0 {
		b.WriteString("\nCOMMITS ATTRIBUTED TO THIS STREAM:\n")
		for _, c := range s.Commits {
			fmt.Fprintf(&b, "- %s@%s %s (+%d/-%d)%s\n", c.Repo, c.SHA, c.Subject, c.Added, c.Deleted,
				map[bool]string{true: "", false: "  [NOT PUSHED]"}[c.State == model.StateShipped])
		}
	} else {
		b.WriteString("\nCOMMITS: none attributed to this stream.\n")
	}
	return b.String()
}

func daySummaries(d model.Day) string {
	var b strings.Builder
	b.WriteString("STREAMS TODAY:\n\n")
	for _, s := range d.Streams {
		if s.Narration == nil {
			continue
		}
		fmt.Fprintf(&b, "%s\n  intent: %s\n  happened: %s\n", s.Title, s.Narration.Intent, s.Narration.Happened)
		for _, x := range s.Narration.Decisions {
			fmt.Fprintf(&b, "  decided: %s\n", x)
		}
		for _, x := range s.Narration.Open {
			fmt.Fprintf(&b, "  open: %s\n", x)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// lastN keeps the tail of a long stream.
//
// The end of a session is where conclusions live — what was fixed, what was
// still broken when work stopped. Truncating from the front would keep the
// setup and throw away the outcome.
func lastN(ns []model.Note, n int) []model.Note {
	if len(ns) <= n {
		return ns
	}
	return ns[len(ns)-n:]
}
