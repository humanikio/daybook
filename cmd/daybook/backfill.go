package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/humanikio/daybook/internal/config"
	"github.com/humanikio/daybook/internal/model"
)

// `daybook backfill` — build the days that happened before you installed it.
//
// Without this the tool is only useful tomorrow. Everything it needs for last
// month is already on disk: the transcripts are there until Claude Code prunes
// them, and git history goes back further still.
//
// Each day is scanned independently against a window ending at that day's
// close, which is the same pipeline a live scan runs — no separate code path
// to drift, and re-running one day later produces the same file.
func cmdBackfill(args []string) error {
	args = flagsFirst(args)
	fs := flag.NewFlagSet("backfill", flag.ContinueOnError)
	from := fs.String("from", "", "first day (YYYY-MM-DD)")
	to := fs.String("to", "", "last day (YYYY-MM-DD, default yesterday)")
	force := fs.Bool("force", false, "rebuild days that already have a report")
	alsoNarrate := fs.Bool("narrate", false, "narrate each day as it is built")
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}

	end := startOfDay(time.Now()).AddDate(0, 0, -1) // yesterday; today is `scan`
	if *to != "" {
		if end, err = time.ParseInLocation("2006-01-02", *to, time.Local); err != nil {
			return fmt.Errorf("--to wants YYYY-MM-DD, got %q", *to)
		}
	}

	var start time.Time
	switch {
	case *from != "":
		if start, err = time.ParseInLocation("2006-01-02", *from, time.Local); err != nil {
			return fmt.Errorf("--from wants YYYY-MM-DD, got %q", *from)
		}
	case len(fs.Args()) > 0:
		n, err := parseDays(fs.Args()[0])
		if err != nil {
			return err
		}
		start = end.AddDate(0, 0, -(n - 1))
	default:
		start = end.AddDate(0, 0, -6) // a week
	}
	if start.After(end) {
		return fmt.Errorf("%s is after %s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	}

	days := int(end.Sub(start).Hours()/24) + 1
	if days > 120 && !*force {
		return fmt.Errorf("that is %d days, and each one costs a git pass over every repo.\n"+
			"  Transcripts only go back so far — try a range you can actually see,\n"+
			"  or pass --force if you really mean it", days)
	}
	// Say how far back the evidence actually goes before spending time on days
	// that cannot produce anything. Claude Code prunes transcripts, so asking
	// for six months yields silent empties without this.
	if oldest, ok := oldestTranscript(cfg); ok {
		fmt.Printf("transcripts go back to %s\n", oldest.Format("Mon 2 Jan"))
		if start.Before(oldest) {
			fmt.Printf("  ! %s to %s will have commits but no sessions\n",
				start.Format("2 Jan"), oldest.AddDate(0, 0, -1).Format("2 Jan"))
		}
	}

	fmt.Printf("\nbuilding %s, about %s\n", plural(days, "day"), estimate(days))
	if !*alsoNarrate && cfg.Narrate.Enabled {
		fmt.Printf("  facts only — add --narrate for prose (about 2 minutes per day)\n")
	}
	fmt.Println()

	built, skipped, empty := 0, 0, 0
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		out := filepath.Join(cfg.OutputsDir(), date+".md")
		if !*force {
			if _, err := os.Stat(out); err == nil {
				skipped++
				fmt.Printf("  %s   already built\n", d.Format("Mon 2 Jan"))
				continue
			}
		}

		day, err := scanAt(cfg, endOfDay(d))
		if err != nil {
			fmt.Printf("  %s  failed: %v\n", d.Format("Mon 2 Jan"), err)
			continue
		}
		if day.Totals.Streams == 0 && day.Totals.Commits == 0 {
			// Write nothing at all. A file saying "you did nothing" is worse
			// than no file: `daybook week` would count it as a day that was
			// measured, and it was not.
			//
			// But SAY so. Printing only the days that produced something meant
			// a long range sat silent for minutes and read as a hang.
			empty++
			fmt.Printf("  %s   —\n", d.Format("Mon 2 Jan"))
			continue
		}
		if err := writeDay(cfg, day); err != nil {
			return err
		}
		built++
		fmt.Printf("  %s  %2d streams · %3d commits · %s\n",
			d.Format("Mon 2 Jan"), day.Totals.Streams, day.Totals.Commits, durStr(day.Totals.ActiveMinutes))

		// Only on request, even when narrate.enabled is set. A live scan
		// narrating one day costs two minutes; a fortnight of backfill doing it
		// silently costs half an hour and a large slice of quota, and nobody
		// asking for their history back is asking for that.
		if *alsoNarrate {
			if err := narrateDay(cfg, &day); err != nil {
				fmt.Fprintf(os.Stderr, "    narration: %v\n", err)
			}
		}
	}

	fmt.Printf("\n%s built", plural(built, "day"))
	if skipped > 0 {
		fmt.Printf(" · %d already had one (--force to rebuild)", skipped)
	}
	if empty > 0 {
		fmt.Printf(" · %d had nothing", empty)
	}
	fmt.Println()
	if built > 0 {
		fmt.Printf("read one:  daybook day %s\n", end.Format("2006-01-02"))
	}
	return nil
}

// parseDays accepts 7, 7d, or 2w — the shapes people type for "how far back".
// estimate sets an expectation before a long wait rather than after it.
func estimate(days int) string {
	secs := days * 2
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	return fmt.Sprintf("%dm", (secs+59)/60)
}

func parseDays(s string) (int, error) {
	mult := 1
	switch {
	case len(s) > 1 && s[len(s)-1] == 'd':
		s = s[:len(s)-1]
	case len(s) > 1 && s[len(s)-1] == 'w':
		s, mult = s[:len(s)-1], 7
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n < 1 {
		return 0, fmt.Errorf("how many days? try `daybook backfill 7` or `daybook backfill 2w`")
	}
	return n * mult, nil
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func endOfDay(t time.Time) time.Time {
	return startOfDay(t).AddDate(0, 0, 1).Add(-time.Nanosecond)
}

// oldestTranscript is the retention edge — how far back this can possibly see.
func oldestTranscript(cfg config.Config) (time.Time, bool) {
	var oldest time.Time
	for _, a := range cfg.Watch.Agents {
		files, _ := filepath.Glob(filepath.Join(config.Expand(a.Path), "*", "*.jsonl"))
		for _, f := range files {
			fi, err := os.Stat(f)
			if err != nil {
				continue
			}
			if oldest.IsZero() || fi.ModTime().Before(oldest) {
				oldest = fi.ModTime()
			}
		}
	}
	return oldest, !oldest.IsZero()
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

var _ = model.Day{}
