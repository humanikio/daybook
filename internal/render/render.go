// Package render turns a day into something to read, and into the facts it was
// read from.
//
// Both are pure functions of model.Day. Markdown is the one people open; JSON
// is what regenerates the markdown when the format changes, which is why the
// JSON is written first and never derived from the prose.
package render

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/humanikio/daybook/internal/config"
	"github.com/humanikio/daybook/internal/model"
)

// JSON is the canonical record.
func JSON(d model.Day) ([]byte, error) { return json.MarshalIndent(d, "", "  ") }

// Markdown renders the report.
//
// Ordering is deliberate: what shipped is at the top because it is the question
// being asked, then each stream in the order it ran, then the things that need
// attention. Detail sinks; state floats.
func Markdown(d model.Day, cfg config.Config) string {
	var b strings.Builder
	t := d.Totals

	fmt.Fprintf(&b, "# %s\n\n", d.WindowEnd.Format("Monday, 2 January 2006"))
	// A weekday alone is ambiguous once the window is a week or more — "Tue
	// 16:51 → Tue 16:51" reads as an instant when it is actually seven days.
	stamp := "Mon 15:04"
	if d.WindowEnd.Sub(d.WindowStart) >= 6*24*time.Hour {
		stamp = "Mon 2 Jan 15:04"
	}
	fmt.Fprintf(&b, "*%s → %s · %s*\n\n",
		d.WindowStart.Format(stamp), d.WindowEnd.Format(stamp), d.Machine)

	fmt.Fprintf(&b, "**%s active** · %d streams · %d prompts · **%d commits** `+%s/-%s` · %d repos\n\n",
		dur(t.ActiveMinutes), t.Streams, t.Prompts, t.Commits,
		comma(t.Added), comma(t.Deleted), t.Repos)

	if len(d.OtherAuthors) > 0 {
		b.WriteString("> **No commits matched your author filter, but this window had commits.**\n>\n")
		b.WriteString("> These identities committed here:\n>\n")
		for _, a := range d.OtherAuthors {
			fmt.Fprintf(&b, "> - `%s`\n", a)
		}
		b.WriteString(">\n> Fix with `daybook config set identity.authors \"a@b.com,c@d.com\"`.\n\n")
	}

	if t.Local > 0 {
		fmt.Fprintf(&b, "> **%d of %d commits have not left this machine.**\n\n", t.Local, t.Commits)
	}
	if t.AgentStreams > 0 {
		fmt.Fprintf(&b, "*%d agent-driven session(s) ran alongside and are excluded from these totals.*\n\n", t.AgentStreams)
	}

	// Prose on top, detail underneath — the order you actually read in. The
	// whole document is re-rendered from the JSON every time, so this section
	// appearing or disappearing is idempotent by construction; there is no
	// block to splice and nothing to duplicate on a re-run.
	if n := d.Narration; n != nil {
		b.WriteString("## Summary\n\n")
		fmt.Fprintf(&b, "%s\n\n", n.Shape)
		if n.Moved != "" {
			fmt.Fprintf(&b, "**Moved.** %s\n\n", n.Moved)
		}
		if n.Carrying != "" {
			fmt.Fprintf(&b, "**Carrying.** %s\n\n", n.Carrying)
		}
	}

	if len(d.Shipped) > 0 {
		b.WriteString("## What shipped\n\n")
		for _, it := range d.Shipped {
			if it.Internal {
				continue // grouped below, so the reader hits the visible work first
			}
			writeShipped(&b, it)
		}
		var internal []model.ShippedItem
		for _, it := range d.Shipped {
			if it.Internal {
				internal = append(internal, it)
			}
		}
		if len(internal) > 0 {
			b.WriteString("### Internal\n\n")
			b.WriteString("*No user-facing surface — refactors, docs, tooling.*\n\n")
			for _, it := range internal {
				writeShipped(&b, it)
			}
		}
	}

	// ---- what shipped, by business or repo ----
	if t.Commits > 0 {
		byGroup := map[string]*group{}
		for _, s := range d.Streams {
			for _, c := range s.Commits {
				addTo(byGroup, cfg, c)
			}
		}
		for _, c := range d.Unattributed {
			addTo(byGroup, cfg, c)
		}
		var keys []string
		for k := range byGroup {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return byGroup[keys[i]].n > byGroup[keys[j]].n })

		b.WriteString("| shipped to | commits | lines | repos |\n|---|--:|--:|---|\n")
		for _, k := range keys {
			g := byGroup[k]
			var rs []string
			for r := range g.repos {
				rs = append(rs, r)
			}
			sort.Strings(rs)
			fmt.Fprintf(&b, "| %s | %d | +%s/-%s | %s |\n",
				k, g.n, comma(g.add), comma(g.del), strings.Join(rs, ", "))
		}
		b.WriteString("\n")
	}

	// ---- streams ----
	b.WriteString("## Streams\n\n")
	for _, s := range d.Streams {
		if s.Agent {
			continue
		}
		fmt.Fprintf(&b, "### %s · %s\n\n", span(s.First, s.Last), s.Title)

		var facts []string
		facts = append(facts, "`"+string(s.State)+"`")
		facts = append(facts, fmt.Sprintf("%d prompts", len(s.Prompts)))
		if s.WorkedMillis > 0 {
			facts = append(facts, dur(int(s.WorkedMillis/60000))+" working")
		}
		if s.Failures > 0 {
			facts = append(facts, fmt.Sprintf("%d failed", s.Failures))
		}
		if s.OutputTokens > 0 {
			facts = append(facts, fmt.Sprintf("%dk tokens", s.OutputTokens/1000))
		}
		if rs := topRepos(s.Repos, 3); rs != "" {
			facts = append(facts, rs)
		}
		fmt.Fprintf(&b, "%s\n\n", strings.Join(facts, " · "))

		if n := s.Narration; n != nil {
			fmt.Fprintf(&b, "%s\n\n", n.Intent)
			fmt.Fprintf(&b, "%s\n\n", n.Happened)
			for _, x := range n.Decisions {
				fmt.Fprintf(&b, "- **Decided.** %s\n", x)
			}
			for _, x := range n.Open {
				fmt.Fprintf(&b, "- **Open.** %s\n", x)
			}
			if len(n.Decisions) > 0 || len(n.Open) > 0 {
				b.WriteString("\n")
			}
		}

		if len(s.Prompts) > 0 {
			b.WriteString("**Asked for**\n\n")
			for _, p := range pickPrompts(s, 3) {
				fmt.Fprintf(&b, "- `%s` %s\n", p.At.Format("15:04"), clip(p.Text, 150))
			}
			if len(s.Prompts) > 3 {
				fmt.Fprintf(&b, "- *…%d more*\n", len(s.Prompts)-3)
			}
			b.WriteString("\n")
		}

		if len(s.Commits) > 0 {
			ex, rp := 0, 0
			for _, c := range s.Commits {
				if c.Confidence == model.ConfExact {
					ex++
				} else {
					rp++
				}
			}
			// The split is always printed. A single clean number when nearly
			// half of it is inference reads as certainty this cannot offer.
			fmt.Fprintf(&b, "**Shipped** (%d exact, %d inferred)\n\n", ex, rp)
			for _, c := range s.Commits {
				mark := ""
				if c.State != model.StateShipped {
					mark = " ⚠ not pushed"
				}
				fmt.Fprintf(&b, "- `%s@%s` %s `+%d/-%d`%s\n", c.Repo, c.SHA, c.Subject, c.Added, c.Deleted, mark)
			}
			b.WriteString("\n")
		} else {
			// Never leave a no-commit stream looking like nothing happened.
			fmt.Fprintf(&b, "*No commits attributed. %d tool calls, %s.*\n\n",
				totalTools(s), plural(len(s.Files), "file", "files"))
		}
	}

	// ---- attention ----
	if len(d.Unattributed) > 0 {
		fmt.Fprintf(&b, "## Unattributed (%d)\n\n", len(d.Unattributed))
		b.WriteString("*Commits no stream could claim. A large bucket here means the join needs work.*\n\n")
		for _, c := range d.Unattributed {
			fmt.Fprintf(&b, "- `%s@%s` %s %s\n", c.Repo, c.SHA, c.At.Format("15:04"), c.Subject)
		}
		b.WriteString("\n")
	}

	// What broke. A report listing only what shipped describes half a day —
	// failures are where the time went, and the part nobody writes down.
	var broke []model.Stream
	totalFail := 0
	for _, s := range d.Streams {
		if s.Agent || s.Failures == 0 {
			continue
		}
		totalFail += s.Failures
		broke = append(broke, s)
	}
	if totalFail > 0 {
		fmt.Fprintf(&b, "## What broke (%d)\n\n", totalFail)
		sort.Slice(broke, func(i, j int) bool { return broke[i].Failures > broke[j].Failures })
		for _, s := range broke {
			fmt.Fprintf(&b, "**%s** · %d\n\n", s.Title, s.Failures)
			for _, f := range s.Failed {
				if len(s.Failed) > 3 {
					break
				}
				fmt.Fprintf(&b, "- `%s`\n", clip(f, 140))
			}
			if len(s.Failed) > 3 {
				for _, f := range s.Failed[:3] {
					fmt.Fprintf(&b, "- `%s`\n", clip(f, 140))
				}
				fmt.Fprintf(&b, "- *…%d more*\n", len(s.Failed)-3)
			}
			b.WriteString("\n")
		}
	}

	var risky []model.RepoState
	for _, r := range d.Repos {
		if r.Ahead > 0 || r.Dirty > 0 {
			risky = append(risky, r)
		}
	}
	if len(risky) > 0 {
		sort.Slice(risky, func(i, j int) bool { return risky[i].Ahead > risky[j].Ahead })
		b.WriteString("## Still on this machine\n\n")
		b.WriteString("*Work nobody else can see yet.*\n\n")
		for _, r := range risky {
			br := r.Branch
			if br == "" {
				br = "detached HEAD"
			}
			// The subjects, not just the count. "3 unpushed" is a number
			// nobody can act on; the subjects are what a teammate would need
			// to know is coming.
			if r.Ahead > 0 {
				fmt.Fprintf(&b, "**%s** — %s on `%s`, not pushed\n\n",
					r.Repo, plural(r.Ahead, "commit", "commits"), br)
				for _, s := range r.AheadSubjects {
					fmt.Fprintf(&b, "- %s\n", code(s))
				}
				b.WriteString("\n")
			}
			if r.Dirty > 0 {
				fmt.Fprintf(&b, "**%s** — %s uncommitted on `%s`\n\n",
					r.Repo, plural(r.Dirty, "file", "files"), br)
				for _, f := range r.DirtyFiles {
					fmt.Fprintf(&b, "- %s\n", code(f))
				}
				b.WriteString("\n")
			}
		}
	}

	if len(d.ClosedToday) > 0 {
		fmt.Fprintf(&b, "## Closed today (%d)\n\n", len(d.ClosedToday))
		// Printed with the evidence so a wrong close is visible the day it
		// happens. False positives are the dangerous direction, and the fix is
		// `daybook reopen <id>`.
		for _, it := range d.ClosedToday {
			ev := ""
			if it.ClosedBy != nil {
				switch it.ClosedBy.Kind {
				case "commit":
					ev = fmt.Sprintf(" — `%s@%s`", it.ClosedBy.Repo, it.ClosedBy.SHA)
				case "session":
					ev = fmt.Sprintf(" — \"%s\"", clip(it.ClosedBy.Quote, 90))
				case "manual":
					ev = " — closed by hand"
				}
			}
			fmt.Fprintf(&b, "- `%s` %s%s\n", it.ID, it.Text, ev)
		}
		b.WriteString("\n")
	}

	if open := d.OpenItems; len(open) > 0 {
		fmt.Fprintf(&b, "## Still open (%d)\n\n", len(open))
		b.WriteString("*Work already done that has not finished proving itself. Oldest first.*\n\n")
		// Capped, because a single real day produced 44 of these and an
		// uncapped list buries the report it is attached to within a week.
		// Oldest first means the cap keeps what is rotting and drops what is
		// merely recent; `daybook open` still shows everything.
		const shown = 15
		trimmed := open
		if len(trimmed) > shown {
			trimmed = trimmed[:shown]
		}
		for _, it := range trimmed {
			age := it.Age(d.WindowEnd)
			tag := ""
			if age >= 14 {
				tag = " ⚠"
			}
			fmt.Fprintf(&b, "- `%s` **%dd**%s %s *(%s)*\n", it.ID, age, tag, it.Text, it.Stream)
		}
		if len(open) > len(trimmed) {
			fmt.Fprintf(&b, "\n*…and %d more. `daybook open` for the full list.*\n", len(open)-len(trimmed))
		}
		b.WriteString("\n")
	}

	if d.ParseErrors > 0 {
		fmt.Fprintf(&b, "---\n\n*%d transcript line(s) could not be parsed. The format is undocumented and moves between Claude Code versions; run `daybook verify` for detail.*\n", d.ParseErrors)
	}
	return b.String()
}

// writeShipped renders one capability: what it is in plain language, how it
// works for whoever has to maintain it, and where to look.
func writeShipped(b *strings.Builder, it model.ShippedItem) {
	fmt.Fprintf(b, "**%s**\n\n", it.What)
	if it.How != "" {
		fmt.Fprintf(b, "%s\n\n", it.How)
	}
	if len(it.Where) > 0 {
		b.WriteString("*Look at:*\n")
		for _, w := range it.Where {
			fmt.Fprintf(b, "- `%s`\n", w)
		}
		b.WriteString("\n")
	}
	if len(it.Commits) > 0 {
		branch := it.Branch
		if branch == "" {
			branch = "?"
		}
		fmt.Fprintf(b, "<sub>`%s` on **%s**</sub>\n\n", strings.Join(it.Commits, "` · `"), branch)
	}
}

type group struct {
	n, add, del int
	repos       map[string]struct{}
}

func addTo(m map[string]*group, cfg config.Config, c model.Commit) {
	k := cfg.BusinessFor(c.Repo)
	if k == "" {
		k = c.Repo
	}
	g := m[k]
	if g == nil {
		g = &group{repos: map[string]struct{}{}}
		m[k] = g
	}
	g.n++
	g.add += c.Added
	g.del += c.Deleted
	g.repos[c.Repo] = struct{}{}
}

// pickPrompts chooses which prompts to show.
//
// Not the first three. The opening prompt of a continuing stream is usually
// "yeah lets do this" — the longest ones carry the intent, so those win, then
// they are put back in time order so the section still reads as a sequence.
func pickPrompts(s model.Stream, n int) []model.Prompt {
	ps := append([]model.Prompt(nil), s.Prompts...)
	sort.SliceStable(ps, func(i, j int) bool { return len(ps[i].Text) > len(ps[j].Text) })
	if len(ps) > n {
		ps = ps[:n]
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].At.Before(ps[j].At) })
	return ps
}

// span prints a time range, and includes the day whenever the range crosses
// one — a stream that ran from 18:21 yesterday to 18:20 today otherwise reads
// as though it ended before it began.
func span(a, b time.Time) string {
	if a.YearDay() == b.YearDay() && a.Year() == b.Year() {
		return a.Format("15:04") + "–" + b.Format("15:04")
	}
	return a.Format("Mon 15:04") + "–" + b.Format("Mon 15:04")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func topRepos(m map[string]int, n int) string {
	type kv struct {
		k string
		v int
	}
	var xs []kv
	for k, v := range m {
		xs = append(xs, kv{k, v})
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i].v > xs[j].v })
	var out []string
	for i, x := range xs {
		if i >= n {
			break
		}
		out = append(out, x.k)
	}
	return strings.Join(out, ", ")
}

func totalTools(s model.Stream) int {
	n := 0
	for _, v := range s.Tools {
		n += v
	}
	return n
}

// code renders a value inside a markdown code span.
//
// Commit subjects are written by people and routinely contain backticks — this
// tool's own do — which close the span early and leave the rest as prose. U+2018
// looks near enough and cannot terminate anything.
func code(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "\u2018") + "`"
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if i := strings.LastIndex(s[:n], " "); i > n/2 {
		return s[:i] + "…"
	}
	return s[:n] + "…"
}

func dur(minutes int) string {
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%.1fh", float64(minutes)/60)
}

func comma(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
