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
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kardianos/service"

	"github.com/tndigitalmark/daybook/internal/config"
	"github.com/tndigitalmark/daybook/internal/derive"
	"github.com/tndigitalmark/daybook/internal/ledger"
	"github.com/tndigitalmark/daybook/internal/model"
	"github.com/tndigitalmark/daybook/internal/narrate"
	"github.com/tndigitalmark/daybook/internal/render"
	"github.com/tndigitalmark/daybook/internal/schedule"
	"github.com/tndigitalmark/daybook/internal/source"
	"github.com/tndigitalmark/daybook/internal/source/claudecode"
	"github.com/tndigitalmark/daybook/internal/svc"
	"github.com/tndigitalmark/daybook/internal/vcs"
	"github.com/tndigitalmark/daybook/internal/wizard"
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
	case "week":
		err = cmdWeek(args)
	case "serve":
		err = cmdServe(args)
	case "service":
		err = cmdService(args)
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
  daybook week [date]     rollup for the week containing date
  daybook serve           run the scheduler (foreground in a terminal)
  daybook service …       install | uninstall | start | stop | restart | status
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
	// scan rebuilds the day from source every time, which would throw away
	// prose already written for it — and `scan` is documented as safe to run as
	// often as you like. Carry the narration across by stream id so re-scanning
	// refreshes the facts without costing the words.
	carryNarration(cfg, &day)

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

// carryNarration copies prose from a previous scan of the SAME day onto the
// freshly derived streams.
//
// Only by stream id, and only where the stream still exists: a stream whose
// facts changed keeps its prose (the words describe work that still happened),
// but one that vanished takes its prose with it rather than being resurrected.
func carryNarration(cfg config.Config, day *model.Day) {
	prev, err := loadDay(cfg, day.Date)
	if err != nil {
		return
	}
	byID := map[string]*model.Narration{}
	for i := range prev.Streams {
		if prev.Streams[i].Narration != nil {
			byID[prev.Streams[i].ID] = prev.Streams[i].Narration
		}
	}
	for i := range day.Streams {
		if n, ok := byID[day.Streams[i].ID]; ok {
			day.Streams[i].Narration = n
		}
	}
	day.Narration = prev.Narration
	day.ClosedToday = prev.ClosedToday
	day.OpenItems = ledger.Open(ledger.Load(cfg))
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
	st := loadLastRun(cfg)
	st.WindowEnd = day.WindowEnd.Format(time.RFC3339)
	st.Date = day.Date
	st.Machine = day.Machine
	return saveLastRun(cfg, st)
}

// lastRun is what makes scan idempotent and catch-up free.
//
// Slot is the scheduled time this machine has already served. The scheduler
// compares it against the most recent slot that has passed, which is why a
// laptop asleep at 23:30 still gets its report on wake instead of silently
// skipping the day.
type lastRun struct {
	WindowEnd string `json:"windowEnd,omitempty"`
	Date      string `json:"date,omitempty"`
	Machine   string `json:"machine,omitempty"`
	Slot      string `json:"slot,omitempty"`
}

func lastRunPath(cfg config.Config) string {
	return filepath.Join(cfg.StateDir(), "last-run.json")
}

func loadLastRun(cfg config.Config) lastRun {
	var st lastRun
	if b, err := os.ReadFile(lastRunPath(cfg)); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func saveLastRun(cfg config.Config, st lastRun) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(lastRunPath(cfg), b)
}

func cmdWeek(args []string) error {
	fs := flag.NewFlagSet("week", flag.ContinueOnError)
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	d, err := resolveDate(fs.Args())
	if err != nil {
		return err
	}
	when, err := time.ParseInLocation("2006-01-02", d, time.Local)
	if err != nil {
		return err
	}
	mon, sun := render.WeekBounds(when)

	var days []model.Day
	for d := mon; !d.After(sun); d = d.AddDate(0, 0, 1) {
		if day, err := loadDay(cfg, d.Format("2006-01-02")); err == nil {
			days = append(days, day)
		}
	}
	if len(days) == 0 {
		return fmt.Errorf("no scanned days between %s and %s", mon.Format("2006-01-02"), sun.Format("2006-01-02"))
	}

	md := render.Week(days, cfg)
	y, wk := when.ISOWeek()
	out := filepath.Join(cfg.OutputsDir(), fmt.Sprintf("%d-W%02d.md", y, wk))
	if err := writeFile(out, []byte(md)); err != nil {
		return err
	}
	fmt.Print(md)
	fmt.Fprintf(os.Stderr, "\nwritten to %s\n", out)
	return nil
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	if c := svc.Conflicts(); len(c) > 0 {
		// A system-level registration can never work — wrong HOME, wrong
		// keychain, wrong git identity — and its symptom is an empty report
		// rather than an error, so it has to be said out loud.
		fmt.Fprintf(os.Stderr, "warning: a system-level service is registered and cannot work:\n  %s\n",
			strings.Join(c, "\n  "))
	}

	s, err := svc.New(cfg, func() error { return serveLoop(cfg) })
	if err != nil {
		return err
	}
	if service.Interactive() {
		next, _ := schedule.Next(cfg, time.Now())
		fmt.Printf("daybook serving · next run %s · ctrl-c to stop\n", next.Format("Mon 15:04"))
	}
	return s.Run()
}

// serveLoop wakes once a minute and asks whether a scheduled slot is owed.
//
// A minute of granularity is plenty for a daily job and keeps the loop trivial:
// no timers to recompute when the clock jumps, no wake notifications to
// subscribe to, and a laptop that slept through its slot simply notices on the
// first tick after waking.
func serveLoop(cfg config.Config) error {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()

	for {
		// Config is re-read every tick rather than captured at start. The run
		// time is the field people change most, and "I changed it and it did
		// not take until I restarted the service" is a bad first bug.
		live, err := config.Load(cfg.Path())
		if err != nil {
			live = cfg
		}

		st := loadLastRun(live)
		var served time.Time
		if st.Slot != "" {
			served, _ = time.Parse(time.RFC3339, st.Slot)
		}

		if slot, due := schedule.Due(live, served, time.Now()); due {
			log.Printf("running for slot %s", slot.Format(time.RFC3339))
			if err := runScheduled(live, slot); err != nil {
				// Do NOT record the slot on failure: leaving it unserved means
				// the next tick tries again, which is what you want for a
				// transient error like a locked git index.
				log.Printf("run failed: %v", err)
			}
		}
		<-tick.C
	}
}

func runScheduled(cfg config.Config, slot time.Time) error {
	day, err := scan(cfg)
	if err != nil {
		return err
	}
	carryNarration(cfg, &day)
	if err := writeDay(cfg, day); err != nil {
		return err
	}
	if cfg.Narrate.Enabled {
		// Narration failing must not un-serve the slot: the report is already
		// on disk and re-running the whole scan tomorrow would not help.
		if err := narrateDay(cfg, &day); err != nil {
			log.Printf("narration: %v", err)
		}
	}

	st := loadLastRun(cfg)
	st.Slot = slot.Format(time.RFC3339)
	if err := saveLastRun(cfg, st); err != nil {
		return err
	}
	log.Printf("wrote %s · %d streams · %d commits", day.Date, day.Totals.Streams, day.Totals.Commits)
	return nil
}

func cmdService(args []string) error {
	fs := flag.NewFlagSet("service", flag.ContinueOnError)
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	rest := fs.Args()
	action := "status"
	if len(rest) > 0 {
		action = rest[0]
	}

	if action == "status" {
		installed, running, err := svc.Status(cfg)
		if err != nil {
			return err
		}
		switch {
		case !installed:
			fmt.Printf("not installed — `daybook service install` registers a %s\n", svc.AutoStartKind())
		case running:
			next, _ := schedule.Next(cfg, time.Now())
			fmt.Printf("installed and running · next run %s\n", next.Format("Mon 15:04"))
		default:
			fmt.Println("installed but stopped — `daybook service start`")
		}
		st := loadLastRun(cfg)
		if st.Slot != "" {
			fmt.Printf("last served slot: %s\n", st.Slot)
		}
		for _, c := range svc.Conflicts() {
			fmt.Printf("! system-level registration that cannot work: %s\n", c)
		}
		return nil
	}

	switch action {
	case "install", "uninstall", "start", "stop", "restart":
		if err := svc.Control(cfg, action); err != nil {
			return err
		}
		fmt.Println(action + "ed")
		if action == "install" {
			if n := svc.PostInstallNote(); n != "" {
				fmt.Println("  " + n)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown action %q — install|uninstall|start|stop|restart|status", action)
	}
}

func cmdNarrate(args []string) error {
	fs := flag.NewFlagSet("narrate", flag.ContinueOnError)
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	date, err := resolveDate(fs.Args())
	if err != nil {
		return err
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

// resolveDate accepts a date, "today", "yesterday", or nothing.
//
// The aliases exist because the two things anyone types are "what did I do
// today" and "what did I do yesterday", and making someone compute a date to
// ask the second one is a small daily insult.
func resolveDate(args []string) (string, error) {
	if len(args) == 0 {
		return time.Now().Format("2006-01-02"), nil
	}
	switch strings.ToLower(args[0]) {
	case "today":
		return time.Now().Format("2006-01-02"), nil
	case "yesterday":
		return time.Now().AddDate(0, 0, -1).Format("2006-01-02"), nil
	}
	if _, err := time.Parse("2006-01-02", args[0]); err != nil {
		return "", fmt.Errorf("want a date like 2026-08-24, or today/yesterday — got %q", args[0])
	}
	return args[0], nil
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
	date, err := resolveDate(fs.Args())
	if err != nil {
		return err
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

	st := loadLastRun(cfg)
	if st.Date != "" {
		fmt.Printf("✓ last run     %s (window ended %s)\n", st.Date, st.WindowEnd)
	} else {
		fmt.Println("  ! never run — `daybook scan`")
	}

	installed, running, _ := svc.Status(cfg)
	switch {
	case installed && running:
		next, _ := schedule.Next(cfg, time.Now())
		fmt.Printf("✓ scheduler    running · next %s\n", next.Format("Mon 15:04"))
	case installed:
		fmt.Println("  ! scheduler installed but stopped — `daybook service start`")
	default:
		fmt.Println("  ! scheduler not installed — `daybook service install` (optional)")
	}
	if st.Slot != "" {
		fmt.Printf("  last slot    %s\n", st.Slot)
	}
	for _, c := range svc.Conflicts() {
		fmt.Printf("✗ a system-level registration exists and cannot work: %s\n", c)
	}

	if _, err := narrate.Resolve(cfg); err != nil {
		fmt.Printf("  ! narration  %v\n", err)
	} else {
		fmt.Printf("✓ narration    available (provider %s, enabled=%v)\n",
			cfg.Narrate.Provider, cfg.Narrate.Enabled)
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
