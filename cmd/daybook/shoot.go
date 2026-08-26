package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/humanikio/daybook/internal/config"
	"github.com/humanikio/daybook/internal/model"
	"github.com/humanikio/daybook/internal/narrate"
	"github.com/humanikio/daybook/internal/preview"
)

// `daybook shoot` — photograph where today's work lives in the product.
//
// Deliberately its own command rather than a step inside scan. It drives your
// real browser, which means it occupies it and acts as you for the duration;
// that is a reasonable thing to kick off and a poor thing to schedule at 22:00.
func cmdShoot(args []string) error {
	args = flagsFirst(args)
	fs := flag.NewFlagSet("shoot", flag.ContinueOnError)
	dry := fs.Bool("dry-run", false, "show what would be done, drive nothing")
	verbose := fs.Bool("verbose", false, "print what the agent said")
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	date, err := resolveDate(fs.Args())
	if err != nil {
		return err
	}

	if !cfg.Preview.Enabled {
		return fmt.Errorf("screenshots are off — `daybook config edit`, or `daybook watch <path> --preview`")
	}
	day, err := loadDay(cfg, date)
	if err != nil {
		return err
	}
	if len(day.Shipped) == 0 {
		return fmt.Errorf("%s has no capability list to illustrate — run `daybook narrate %s` first", date, date)
	}

	// Clean up anything a previous run left behind before starting more. A
	// crashed run cannot tidy after itself, and the pid files are how this one
	// finds out.
	for _, r := range preview.ReapOrphans(cfg.StateDir()) {
		fmt.Printf("  stopped a leftover server: %s\n", r)
	}

	// Only servers under a root that opted in — the second gate, applied to the
	// thing it gates — and then one per repository, because the catalogue keys
	// on wherever a session happened to be sitting.
	var covered []preview.Server
	for _, s := range day.Servers {
		if cfg.PreviewCovers(s.Dir) {
			covered = append(covered, previewServer(s))
		}
	}
	var wanted []model.Server
	for _, s := range preview.PickPerRepo(covered) {
		wanted = append(wanted, model.Server{
			Command: s.Command, Dir: s.Dir, BootSeconds: s.BootSeconds,
			Port: s.Port, At: s.At, Repo: s.Repo,
		})
	}
	if len(wanted) == 0 {
		return fmt.Errorf("no server was recorded in a folder marked for screenshots\n" +
			"  `daybook watch` shows which folders are marked")
	}

	// Only capabilities that shipped in a repo one of these servers actually
	// serves.
	//
	// Without this the agent is handed today's best work and today's running
	// apps as two unrelated lists, and asked to find one in the other. A dry
	// run made the failure obvious: three servers for one product, six
	// capabilities from a different one, and an instruction to go photograph
	// them. It would have found something — a wrong screen, confidently
	// captured, which is the outcome this whole feature has to avoid.
	serving := map[string]bool{}
	for _, s := range wanted {
		if s.Repo != "" {
			serving[s.Repo] = true
		}
	}
	var caps []string
	for _, it := range day.Shipped {
		if it.Internal {
			continue
		}
		for _, c := range it.Commits {
			repo, _, ok := strings.Cut(c, "@")
			if ok && serving[strings.TrimSpace(repo)] {
				caps = append(caps, it.What)
				break
			}
		}
	}
	if len(caps) == 0 {
		var served, shipped []string
		for r := range serving {
			served = append(served, r)
		}
		for _, it := range day.Shipped {
			for _, c := range it.Commits {
				if r, _, ok := strings.Cut(c, "@"); ok {
					shipped = appendOnce(shipped, strings.TrimSpace(r))
				}
			}
		}
		return fmt.Errorf("nothing that shipped on %s runs in a folder marked for screenshots\n"+
			"  serving: %s\n  shipped in: %s",
			date, strings.Join(served, ", "), strings.Join(shipped, ", "))
	}
	if len(caps) > cfg.Preview.MaxPhotos {
		caps = caps[:cfg.Preview.MaxPhotos]
	}

	// Start only what the chosen capabilities need. Booting an app that hosts
	// none of them is a minute of waiting and a process to clean up, for
	// nothing.
	needed := map[string]bool{}
	for _, it := range day.Shipped {
		if it.Internal {
			continue
		}
		if !containsStr(caps, it.What) {
			continue
		}
		for _, c := range it.Commits {
			if r, _, ok := strings.Cut(c, "@"); ok {
				needed[strings.TrimSpace(r)] = true
			}
		}
	}
	var use []model.Server
	for _, s := range wanted {
		if needed[s.Repo] {
			use = append(use, s)
		}
	}
	wanted = use

	assets := filepath.Join(cfg.OutputRoot(), "assets", date)
	req := preview.CaptureRequest{Capabilities: caps, Dir: assets, Max: cfg.Preview.MaxPhotos}

	// Start, or find, whatever has to be serving.
	var handles []*preview.Handle
	defer func() {
		for _, h := range handles {
			if h.Started() {
				fmt.Printf("  stopping %s\n", h.Server.Command)
				h.Stop()
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), cfg.PreviewTimeout()+10*time.Minute)
	defer cancel()

	for _, s := range wanted {
		switch {
		case preview.Reachable(s.Port):
			fmt.Printf("  ✓ %s already serving on :%d\n", s.Repo, s.Port)
			req.Running = append(req.Running, fmt.Sprintf("%s on http://localhost:%d", s.Repo, s.Port))
		case *dry:
			fmt.Printf("  · would start `%s` in %s (wait %s)\n", s.Command, s.Dir, preview.BootWaitOf(s))
		case !cfg.Preview.StartServers:
			// Not an error. Using what is already up is the whole low-risk
			// path, and refusing to run because one app is down would throw
			// away the others.
			fmt.Printf("  ! %s is not running and start_servers is off — skipping it\n", s.Repo)
		default:
			fmt.Printf("  starting `%s` in %s…\n", s.Command, filepath.Base(s.Dir))
			h, err := preview.Start(ctx, previewServer(s), cfg.StateDir())
			if err != nil {
				fmt.Fprintf(os.Stderr, "    could not: %v\n", err)
				continue
			}
			handles = append(handles, h)
			// The port the handle RESOLVED, not the one the catalogue guessed.
			// A server announces where it landed and that is frequently not
			// where it was recorded — and telling the agent to visit
			// http://localhost:0 is a guaranteed empty result.
			port := h.Port()
			if port <= 0 {
				port = s.Port
			}
			if port <= 0 {
				fmt.Printf("    started, but it never said which port — cannot point the browser at it\n")
				continue
			}
			req.Running = append(req.Running, fmt.Sprintf("%s on http://localhost:%d", s.Repo, port))
			fmt.Printf("    up on :%d\n", port)
		}
	}

	if *dry {
		fmt.Printf("\n  would photograph up to %d of:\n", cfg.Preview.MaxPhotos)
		for _, c := range caps {
			fmt.Printf("    · %s\n", clipStr(c, 76))
		}
		fmt.Printf("\n  and would send this to the agent:\n\n%s\n", indent(req.Prompt()))
		return nil
	}
	if len(req.Running) == 0 {
		return fmt.Errorf("nothing is serving, so there is nothing to look at")
	}

	run, err := narrate.BrowserRunner(cfg)
	if err != nil {
		return fmt.Errorf("cannot drive the browser: %w", err)
	}
	fmt.Printf("\n  looking at %s…\n", plural(len(caps), "capability", "capabilities"))

	if *verbose {
		run = logging(run)
	}
	shots, err := preview.Capture(ctx, run, req)
	if err != nil {
		return err
	}
	if len(shots) == 0 {
		// A legitimate outcome, and better than a wrong picture.
		fmt.Println("  nothing worth photographing was reachable")
		return nil
	}

	day.Shots = shots
	if err := writeDay(cfg, day); err != nil {
		return err
	}
	fmt.Printf("\n  %s:\n", plural(len(shots), "screenshot"))
	for _, s := range shots {
		fmt.Printf("    %s  %s\n", s.File, clipStr(s.Note, 60))
	}
	fmt.Printf("  %s\n", assets)
	return nil
}

// logging wraps the runner so `--verbose` shows what was asked and what came
// back. An empty result is otherwise indistinguishable from a broken one.
func logging(run preview.Runner) preview.Runner {
	return func(ctx context.Context, system, prompt string) (string, error) {
		fmt.Printf("\n---- prompt ----\n%s\n---- reply ----\n", prompt)
		out, err := run(ctx, system, prompt)
		if err != nil {
			fmt.Printf("(error) %v\n", err)
		} else {
			fmt.Println(out)
		}
		fmt.Println("----------------")
		return out, err
	}
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func appendOnce(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

func previewServer(s model.Server) preview.Server {
	return preview.Server{
		Command: s.Command, Dir: s.Dir, BootSeconds: s.BootSeconds,
		Port: s.Port, At: s.At, Repo: s.Repo,
	}
}

func indent(s string) string {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("    " + l + "\n")
	}
	return b.String()
}

var _ = config.Config{}
