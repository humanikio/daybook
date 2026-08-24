// Command daybook reports what you actually worked on each day, read from your
// Claude Code transcripts and joined against what genuinely shipped.
//
// Nothing here holds logic: main parses flags and dispatches. Everything real
// lives in internal/, so wrapping this in a server later is a new cmd/, not a
// rewrite.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tndigitalmark/claude-code-daybook/internal/config"
	"github.com/tndigitalmark/claude-code-daybook/internal/derive"
	"github.com/tndigitalmark/claude-code-daybook/internal/ledger"
	"github.com/tndigitalmark/claude-code-daybook/internal/model"
	"github.com/tndigitalmark/claude-code-daybook/internal/narrate"
	"github.com/tndigitalmark/claude-code-daybook/internal/render"
	"github.com/tndigitalmark/claude-code-daybook/internal/source"
	"github.com/tndigitalmark/claude-code-daybook/internal/source/claudecode"
	"github.com/tndigitalmark/claude-code-daybook/internal/vcs"
	"github.com/tndigitalmark/claude-code-daybook/internal/wizard"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "init":
		err = cmdInit(args)
	case "scan":
		err = cmdScan(args)
	case "day":
		err = cmdDay(args)
	case "narrate":
		err = cmdNarrate(args)
	case "open":
		err = cmdOpen(args)
	case "close":
		err = cmdClose(args, false)
	case "reopen":
		err = cmdClose(args, true)
	case "verify":
		err = cmdVerify(args)
	case "version", "--version", "-v":
		fmt.Println("daybook", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "daybook: unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "daybook:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`daybook — what you actually got done today, and what of it shipped.

  daybook init            guided setup; writes ~/.daybook/config.yaml
  daybook scan            read the last window, join against git, write the report
  daybook day [date]      print a report (default: today)
  daybook narrate [date]  add prose and reconcile the open ledger
  daybook open            work that has not finished proving itself
  daybook close <id>      close a ledger item by hand
  daybook reopen <id>     undo a close
  daybook verify          check config, sources, repos and parse health
  daybook version

Flags:
  --config PATH           config file (default ~/.daybook/config.yaml)
  --window DURATION       override window.length for this run
  --stdout                print instead of writing (scan)
  --narrate               narrate immediately after scanning
`)
}

func loadCfg(fs *flag.FlagSet, args []string) (config.Config, *string, error) {
	path := fs.String("config", "", "config file")
	win := fs.String("window", "", "override window length")
	if err := fs.Parse(args); err != nil {
		return config.Config{}, nil, err
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return cfg, nil, err
	}
	if *win != "" {
		cfg.Window.Length = *win
		if err := cfg.Validate(); err != nil {
			return cfg, nil, err
		}
	}
	return cfg, win, nil
}

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	force := fs.Bool("force", false, "overwrite an existing config")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return wizard.Run(*force)
}

func cmdScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	toStdout := fs.Bool("stdout", false, "print instead of writing")
	alsoNarrate := fs.Bool("narrate", false, "narrate after scanning")
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}

	day, err := scan(cfg)
	if err != nil {
		return err
	}

	if *toStdout {
		fmt.Print(render.Markdown(day, cfg))
		return nil
	}
	// The deterministic report is written FIRST, unconditionally. Narration
	// then rewrites both files from the same facts. Nothing a model does can
	// cost you the day.
	if err := writeDay(cfg, day); err != nil {
		return err
	}
	fmt.Printf("%s  %d streams · %d commits (%d not pushed) · %s active\n",
		filepath.Join(cfg.OutputsDir(), day.Date+".md"),
		day.Totals.Streams, day.Totals.Commits, day.Totals.Local, durStr(day.Totals.ActiveMinutes))

	if *alsoNarrate || cfg.Narrate.Enabled {
		return narrateDay(cfg, &day)
	}
	return nil
}

// writeDay puts the facts down before the prose, always in that order.
func writeDay(cfg config.Config, day model.Day) error {
	raw, err := render.JSON(day)
	if err != nil {
		return err
	}
	// Facts first, prose second. The markdown is regenerable from the JSON;
	// the JSON is not regenerable from anything.
	if err := writeFile(filepath.Join(cfg.RawDir(), day.Date+"."+day.Machine+".json"), raw); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(cfg.OutputsDir(), day.Date+".md"),
		[]byte(render.Markdown(day, cfg))); err != nil {
		return err
	}
	return writeFile(filepath.Join(cfg.StateDir(), "last-run.json"),
		[]byte(fmt.Sprintf("{\n  \"windowEnd\": %q,\n  \"date\": %q,\n  \"machine\": %q\n}\n",
			day.WindowEnd.Format(time.RFC3339), day.Date, day.Machine)))
}

func cmdNarrate(args []string) error {
	fs := flag.NewFlagSet("narrate", flag.ContinueOnError)
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	date := time.Now().Format("2006-01-02")
	if rest := fs.Args(); len(rest) > 0 {
		date = rest[0]
	}
	day, err := loadDay(cfg, date)
	if err != nil {
		return err
	}
	return narrateDay(cfg, &day)
}

// narrateDay adds prose, folds the day's open items into the ledger, asks what
// today closed, and rewrites both files from the updated facts.
func narrateDay(cfg config.Config, day *model.Day) error {
	p, err := narrate.Resolve(cfg)
	if err != nil {
		// Not fatal, and never silent: the report already exists, and this is
		// the only place anyone finds out narration did not run.
		fmt.Fprintf(os.Stderr, "narration skipped: %v\n", err)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.NarrateTimeout())
	defer cancel()

	res, err := narrate.Run(ctx, cfg, day, carryForward(cfg, day.Date))
	if err != nil {
		fmt.Fprintf(os.Stderr, "narration skipped: %v\n", err)
		return nil
	}

	items := ledger.Merge(ledger.Load(cfg), *day)
	items, closed := ledger.Judge(ctx, p, items, *day, day.WindowEnd)
	if err := ledger.Save(cfg, items); err != nil {
		return err
	}
	day.OpenItems = ledger.Open(items)
	day.ClosedToday = closed

	if err := writeDay(cfg, *day); err != nil {
		return err
	}
	fmt.Printf("narrated %d stream(s) via %s", res.Streams, res.Provider)
	if res.Rejected > 0 {
		fmt.Printf(" · %d rejected by the verification gate", res.Rejected)
	}
	if res.Failed > 0 {
		fmt.Printf(" · %d failed", res.Failed)
	}
	fmt.Printf(" · %d open, %d closed today\n", len(day.OpenItems), len(closed))
	return nil
}

// carryForward is yesterday's last line per stream, so a long-running stream
// keeps its thread without anyone re-reading its history.
func carryForward(cfg config.Config, date string) map[string]string {
	out := map[string]string{}
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return out
	}
	prev, err := loadDay(cfg, d.AddDate(0, 0, -1).Format("2006-01-02"))
	if err != nil {
		return out
	}
	for _, s := range prev.Streams {
		if s.Narration != nil && s.Narration.CarryForward != "" {
			out[s.ID] = s.Narration.CarryForward
		}
	}
	return out
}

func loadDay(cfg config.Config, date string) (model.Day, error) {
	var day model.Day
	p := filepath.Join(cfg.RawDir(), date+"."+cfg.Machine()+".json")
	b, err := os.ReadFile(p)
	if err != nil {
		return day, fmt.Errorf("no scan for %s — run `daybook scan` first", date)
	}
	if err := json.Unmarshal(b, &day); err != nil {
		return day, fmt.Errorf("%s: %w", p, err)
	}
	return day, nil
}

func cmdOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ContinueOnError)
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	items := ledger.Open(ledger.Load(cfg))
	if len(items) == 0 {
		fmt.Println("nothing open.")
		return nil
	}
	now := time.Now()
	for _, it := range items {
		fmt.Printf("%-8s %3dd  %-28s %s\n", it.ID, it.Age(now), clipStr(it.Stream, 28), it.Text)
	}
	fmt.Printf("\n%d open. close one:  daybook close <id>\n", len(items))
	return nil
}

func cmdClose(args []string, reopen bool) error {
	fs := flag.NewFlagSet("close", flag.ContinueOnError)
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("which item? see `daybook open`")
	}
	items := ledger.Load(cfg)
	var ok bool
	if reopen {
		items, ok = ledger.Reopen(items, rest[0])
	} else {
		items, ok = ledger.Close(items, rest[0], model.Evidence{Kind: "manual"}, time.Now())
	}
	if !ok {
		return fmt.Errorf("no matching item %q", rest[0])
	}
	if err := ledger.Save(cfg, items); err != nil {
		return err
	}
	fmt.Println(map[bool]string{true: "reopened", false: "closed"}[reopen], rest[0])
	return nil
}

func clipStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func cmdDay(args []string) error {
	fs := flag.NewFlagSet("day", flag.ContinueOnError)
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	date := time.Now().Format("2006-01-02")
	if rest := fs.Args(); len(rest) > 0 {
		date = rest[0]
	}
	b, err := os.ReadFile(filepath.Join(cfg.OutputsDir(), date+".md"))
	if err != nil {
		return fmt.Errorf("no report for %s — run `daybook scan` first", date)
	}
	fmt.Print(string(b))
	return nil
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		fmt.Println("✗ config:", err)
		return nil
	}
	fmt.Println("✓ config:", cfg.Path())
	length, _ := cfg.WindowLength()
	fmt.Printf("  window       %s (scope %s)\n", length, cfg.Window.Scope)
	fmt.Printf("  schedule     %s  catch_up=%v\n", cfg.Schedule.At, cfg.Schedule.CatchUp)
	fmt.Printf("  output       %s\n", cfg.OutputRoot())
	fmt.Printf("  machine      %s\n", cfg.Machine())
	fmt.Printf("  no_remote    %s\n", cfg.Output.NoRemote)

	authors := cfg.Identity.Authors
	if len(authors) == 0 {
		authors = config.DetectAuthors()
		fmt.Printf("  authors      (detected) %s\n", strings.Join(authors, ", "))
		if len(authors) == 0 {
			fmt.Println("  ! no author set and none detected — every commit will count as yours")
		}
	} else {
		fmt.Printf("  authors      %s\n", strings.Join(authors, ", "))
	}

	for _, a := range cfg.Watch.Agents {
		p := config.Expand(a.Path)
		n, err := countTranscripts(p)
		if err != nil {
			fmt.Printf("✗ source %s: %v\n", a.Source, err)
			continue
		}
		fmt.Printf("✓ source %-12s %d transcripts at %s\n", a.Source, n, p)
	}

	repos := vcs.Discover(cfg)
	fmt.Printf("✓ repos        %d discovered\n", len(repos))
	noRemote := 0
	for _, r := range repos {
		st := vcs.Status(r.Root, false)
		if !st.HasRemote {
			noRemote++
		}
	}
	if noRemote > 0 {
		fmt.Printf("  ! %d repo(s) have no remote — treated as %q\n", noRemote, cfg.Output.NoRemote)
	}

	if b, err := os.ReadFile(filepath.Join(cfg.StateDir(), "last-run.json")); err == nil {
		fmt.Printf("✓ last run     %s", strings.ReplaceAll(strings.TrimSpace(string(b)), "\n", ""))
		fmt.Println()
	} else {
		fmt.Println("  ! never run — `daybook scan`")
	}
	return nil
}

func countTranscripts(root string) (int, error) {
	m, err := filepath.Glob(filepath.Join(root, "*", "*.jsonl"))
	if err != nil {
		return 0, err
	}
	if len(m) == 0 {
		if _, err := os.Stat(root); err != nil {
			return 0, fmt.Errorf("not found: %s", root)
		}
	}
	return len(m), nil
}

func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// 0600: this file carries prompt text. It is not for other users.
	return os.WriteFile(path, b, 0o600)
}

func durStr(minutes int) string {
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%.1fh", float64(minutes)/60)
}

// ---- pipeline ---------------------------------------------------------------

// scan is the whole pipeline: discover, extract, join, resolve.
func scan(cfg config.Config) (model.Day, error) {
	length, err := cfg.WindowLength()
	if err != nil {
		return model.Day{}, err
	}
	end := time.Now()
	start := end.Add(-length)

	src := claudecode.Source{}
	res, err := src.Streams(cfg, source.Window{Start: start, End: end, Scope: cfg.Window.Scope})
	if err != nil {
		return model.Day{}, err
	}

	repos := vcs.Discover(cfg)
	derive.SetRepoRoots(repos)

	authors := cfg.Identity.Authors
	if len(authors) == 0 {
		authors = config.DetectAuthors()
	}

	// Each repo costs roughly six git invocations, and 44 of them serially was
	// most of the wall time. The work is independent per repo and dominated by
	// process spawn rather than CPU, so a small pool is the whole fix.
	type repoResult struct {
		state   model.RepoState
		commits []model.Commit
	}
	results := make([]repoResult, len(repos))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func(i int, r vcs.Repo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i].state = vcs.Status(r.Root, cfg.Watch.Fetch)
			if cs, err := vcs.Log(r.Root, start, end, authors); err == nil {
				results[i].commits = cs
			}
			// An unreadable repo is not a failed day.
		}(i, r)
	}
	wg.Wait()

	var commits []model.Commit
	var states []model.RepoState
	for _, rr := range results {
		states = append(states, rr.state)
		commits = append(commits, rr.commits...)
	}

	return derive.Build(derive.Input{
		Cfg:         cfg,
		Streams:     res.Streams,
		Commits:     commits,
		Repos:       states,
		WindowStart: start,
		WindowEnd:   end,
		ParseErrors: res.ParseErrors,
	}), nil
}
