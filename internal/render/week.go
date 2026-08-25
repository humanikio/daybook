package render

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/humanikio/daybook/internal/config"
	"github.com/humanikio/daybook/internal/model"
)

// Week renders a rollup over whole days.
//
// It reads only what the daily reports already established — no re-derivation,
// no second pass over transcripts. A week that disagreed with its own days
// would be a bug with nowhere to look for it.
func Week(days []model.Day, cfg config.Config) string {
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	if len(days) == 0 {
		return "# No days scanned in this week.\n"
	}

	var b strings.Builder
	y, wk := days[len(days)-1].WindowEnd.ISOWeek()
	fmt.Fprintf(&b, "# Week %d, %d\n\n", wk, y)
	fmt.Fprintf(&b, "*%s → %s*\n\n", days[0].Date, days[len(days)-1].Date)

	var tot model.Totals
	repos := map[string]struct{}{}
	for _, d := range days {
		tot.ActiveMinutes += d.Totals.ActiveMinutes
		tot.Streams += d.Totals.Streams
		tot.Prompts += d.Totals.Prompts
		tot.Commits += d.Totals.Commits
		tot.Shipped += d.Totals.Shipped
		tot.Local += d.Totals.Local
		tot.Added += d.Totals.Added
		tot.Deleted += d.Totals.Deleted
		for _, s := range d.Streams {
			for _, c := range s.Commits {
				repos[c.Repo] = struct{}{}
			}
		}
	}
	fmt.Fprintf(&b, "**%s active** · %d prompts · **%d commits** `+%s/-%s` · %d repos\n\n",
		dur(tot.ActiveMinutes), tot.Prompts, tot.Commits, comma(tot.Added), comma(tot.Deleted), len(repos))

	// Per-day, because the shape of a week is the thing a total hides. Commits
	// per hour swings by more than tenfold between a design day and a build
	// day, and a single weekly number reports neither.
	b.WriteString("| day | active | streams | prompts | commits | lines |\n|---|--:|--:|--:|--:|--:|\n")
	for _, d := range days {
		t := d.Totals
		wd := d.WindowEnd.Format("Mon 02")
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | +%s/-%s |\n",
			wd, dur(t.ActiveMinutes), t.Streams, t.Prompts, t.Commits, comma(t.Added), comma(t.Deleted))
	}
	fmt.Fprintf(&b, "| **total** | **%s** | | **%d** | **%d** | **+%s/-%s** |\n\n",
		dur(tot.ActiveMinutes), tot.Prompts, tot.Commits, comma(tot.Added), comma(tot.Deleted))

	if tot.Local > 0 {
		fmt.Fprintf(&b, "> **%d of %d commits never left the machine this week.**\n\n", tot.Local, tot.Commits)
	}

	// Streams carried across days, longest first: what a week was actually
	// about, as opposed to what any one day looked like.
	type span struct {
		title string
		days  int
		cmts  int
	}
	byTitle := map[string]*span{}
	for _, d := range days {
		for _, s := range d.Streams {
			if s.Agent {
				continue
			}
			sp := byTitle[s.Title]
			if sp == nil {
				sp = &span{title: s.Title}
				byTitle[s.Title] = sp
			}
			sp.days++
			sp.cmts += len(s.Commits)
		}
	}
	var spans []*span
	for _, sp := range byTitle {
		spans = append(spans, sp)
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].days != spans[j].days {
			return spans[i].days > spans[j].days
		}
		return spans[i].cmts > spans[j].cmts
	})
	b.WriteString("## Streams\n\n| stream | days | commits |\n|---|--:|--:|\n")
	for _, sp := range spans {
		fmt.Fprintf(&b, "| %s | %d | %d |\n", sp.title, sp.days, sp.cmts)
	}
	b.WriteString("\n")

	// Each day's one-line shape, when narration ran. This is the only place a
	// week reads as a narrative rather than a spreadsheet.
	var any bool
	for _, d := range days {
		if d.Narration != nil {
			any = true
			break
		}
	}
	if any {
		b.WriteString("## Days\n\n")
		for _, d := range days {
			if d.Narration == nil {
				continue
			}
			fmt.Fprintf(&b, "**%s** — %s\n\n", d.WindowEnd.Format("Monday 2 January"), d.Narration.Shape)
		}
	}

	if last := days[len(days)-1]; len(last.OpenItems) > 0 {
		fmt.Fprintf(&b, "## Still open (%d)\n\n", len(last.OpenItems))
		shown := last.OpenItems
		if len(shown) > 20 {
			shown = shown[:20]
		}
		for _, it := range shown {
			fmt.Fprintf(&b, "- `%s` **%dd** %s\n", it.ID, it.Age(last.WindowEnd), it.Text)
		}
		if len(last.OpenItems) > len(shown) {
			fmt.Fprintf(&b, "\n*…and %d more.*\n", len(last.OpenItems)-len(shown))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// WeekBounds returns the Monday-to-Sunday range containing t.
func WeekBounds(t time.Time) (time.Time, time.Time) {
	off := (int(t.Weekday()) + 6) % 7 // Monday = 0
	mon := time.Date(t.Year(), t.Month(), t.Day()-off, 0, 0, 0, 0, t.Location())
	return mon, mon.AddDate(0, 0, 6)
}
