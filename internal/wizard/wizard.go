// Package wizard is the guided first-run setup.
//
// DESIGN RULES, taken from a sibling project because they were learned the hard way:
//   - It never installs anything. It PRINTS the command. Executing an
//     installer on someone's machine is not a thing a config-driven tool does.
//   - It never writes credentials. Claude Code owns its own; this only checks.
//   - It is re-runnable. An existing config is detected and kept unless --force.
//   - It degrades on non-interactive stdin, so `daybook init < /dev/null` in a
//     provisioning script writes sane defaults instead of hanging.
//   - Colour and glyphs are gated on the terminal actually rendering them.
package wizard

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/humanikio/daybook/internal/config"
	"github.com/humanikio/daybook/internal/svc"
	"github.com/humanikio/daybook/internal/vcs"
)

// colour is off unless the terminal will actually render it. NO_COLOR and a
// dumb or absent TERM all mean plain text — on a console without VT processing
// the escapes print literally, and the first thing a new user sees is garbage.
var useColor = func() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	t := os.Getenv("TERM")
	if t == "" || t == "dumb" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}()

func c(seq string) string {
	if !useColor {
		return ""
	}
	return seq
}

var (
	bold  = c("\033[1m")
	dim   = c("\033[2m")
	cyan  = c("\033[36m")
	reset = c("\033[0m")
)

// Glyphs fall back to ASCII wherever colour is unavailable — the same set of
// terminals renders ✓ as mojibake under an OEM code page, and changing a
// user's console code page from a setup wizard is not acceptable.
func mark(ok, warn bool) string {
	switch {
	case ok && useColor:
		return "\033[32m✓\033[0m"
	case ok:
		return "[ok]"
	case warn && useColor:
		return "\033[33m!\033[0m"
	case warn:
		return "!"
	case useColor:
		return "\033[31m✗\033[0m"
	default:
		return "[x]"
	}
}

func step(n, total int, title string) {
	fmt.Printf("\n%s%s[%d/%d] %s%s\n", bold, cyan, n, total, title, reset)
}
func ok(f string, a ...any)   { fmt.Printf("      %s %s\n", mark(true, false), fmt.Sprintf(f, a...)) }
func warn(f string, a ...any) { fmt.Printf("      %s %s\n", mark(false, true), fmt.Sprintf(f, a...)) }
func note(f string, a ...any) { fmt.Printf("        %s%s%s\n", dim, fmt.Sprintf(f, a...), reset) }

const total = 5

// Run walks setup and writes the config.
func Run(force bool) error {
	in := bufio.NewReader(os.Stdin)
	interactive := stdinIsInteractive()

	fmt.Printf("\n%sdaybook%s — what you actually got done today, and what of it shipped.\n", bold, reset)

	path := config.File()
	if _, err := os.Stat(path); err == nil && !force {
		fmt.Printf("\n%s already exists.\n", path)
		if !interactive || strings.ToLower(ask(in, "  overwrite it? [y/N]: ", "n")) != "y" {
			note("keeping it. re-run with --force to replace.")
			return nil
		}
	}

	cfg := config.DefaultWithEnv()

	// ---------------------------------------------------------------- 1
	step(1, total, "Checking this machine")

	if p, err := exec.LookPath("claude"); err == nil {
		v := firstLine(tryRun(p, "--version"))
		if v == "" {
			v = "version unknown"
		}
		ok("claude found — %s", v)
		// Deliberately does NOT claim you are signed in. `claude doctor` exits 0
		// regardless of credential state, so sign-in cannot be checked here
		// without burning a real request. Detection is reactive: narration
		// reports it, and the message has to be good because that is the only
		// place anyone finds out.
		note("sign-in not verified here — narration will say so if it needs a login")
	} else {
		warn("claude not found on PATH — narration will be unavailable")
		note("the deterministic report does not need it; everything else works")
	}

	if _, err := exec.LookPath("git"); err == nil {
		ok("git — %s", firstLine(tryRun("git", "--version")))
	} else {
		return fmt.Errorf("git is required and was not found on PATH")
	}

	if _, err := exec.LookPath("gh"); err == nil {
		ok("gh found — pull-request enrichment available")
	} else {
		warn("gh not found — pull-request enrichment off (entirely optional)")
	}

	src := config.Expand(cfg.Watch.Agents[0].Path)
	n, _ := filepath.Glob(filepath.Join(src, "*", "*.jsonl"))
	if len(n) == 0 {
		warn("no transcripts found at %s", src)
		note("if Claude Code stores yours elsewhere, set watch.agents[].path")
	} else {
		ok("%d transcripts at %s", len(n), src)
	}

	// ---------------------------------------------------------------- 2
	step(2, total, "What should we watch?")

	def := defaultRepoRoot()
	root := def
	if stdinIsInteractive() {
		root = ask(in, fmt.Sprintf("      repo roots [%s]: ", def), def)
	}
	for _, r := range strings.Split(root, ",") {
		if r = strings.TrimSpace(r); r != "" {
			cfg.Watch.Repos = append(cfg.Watch.Repos, config.RepoRoot{Path: r, Depth: 4})
		}
	}
	t0 := time.Now()
	repos := vcs.Discover(cfg)
	ok("%d repos found (%dms)", len(repos), time.Since(t0).Milliseconds())

	noRemote := 0
	for _, r := range repos {
		if !vcs.Status(r.Root, false).HasRemote {
			noRemote++
		}
	}
	if noRemote > 0 {
		warn("%d repo(s) have no remote", noRemote)
		note("\"shipped\" means off this machine, so those have no bar to clear;")
		note("output.no_remote=%q treats a commit as done for them", cfg.Output.NoRemote)
	}

	authors := config.DetectAuthors()
	defAuthor := ""
	if len(authors) > 0 {
		defAuthor = authors[0]
	}
	email := defAuthor
	if stdinIsInteractive() {
		email = ask(in, fmt.Sprintf("      your commit email [%s]: ", defAuthor), defAuthor)
	}
	if email != "" {
		cfg.Identity.Authors = []string{email}
		ok("counting commits by %s", email)
	} else {
		warn("no author set — every commit in these repos will count as yours")
	}

	// ---------------------------------------------------------------- 3
	step(3, total, "When should the summary run?")

	at := cfg.Schedule.At
	if stdinIsInteractive() {
		at = ask(in, fmt.Sprintf("      time [%s]: ", cfg.Schedule.At), cfg.Schedule.At)
		if _, err := time.Parse("15:04", at); err != nil {
			warn("%q is not HH:MM — keeping %s", at, cfg.Schedule.At)
			at = cfg.Schedule.At
		}
	}
	cfg.Schedule.At = at
	ok("daily at %s", at)

	if stdinIsInteractive() {
		if strings.ToLower(ask(in, "      run it late if the machine was asleep then? [Y/n]: ", "y")) == "n" {
			cfg.Schedule.CatchUp = false
		}
	}
	if cfg.Schedule.CatchUp {
		note("asleep at %s? it runs on wake rather than skipping the day", at)
	} else {
		note("a missed run is skipped rather than produced late")
	}

	// ---------------------------------------------------------------- 4
	step(4, total, "Writing config")

	body := config.Render(cfg)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return err
	}
	ok("%s", path)
	for _, d := range []string{cfg.OutputsDir(), cfg.RawDir(), cfg.StateDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	ok("output → %s", cfg.OutputRoot())
	note("this directory holds prompt text. keep it private.")

	// ---------------------------------------------------------------- 5
	step(5, total, "Schedule it")
	offerService(in, cfg, interactive)

	fmt.Printf("\n  %snext%s\n", bold, reset)
	fmt.Printf("    daybook scan     run one now against the last %s\n", cfg.Window.Length)
	fmt.Printf("    daybook day      read it\n")
	fmt.Printf("    daybook verify   check everything is wired up\n\n")
	return nil
}

// offerService installs the auto-start registration, or explains why not.
//
// Follows the same rule as everything else here: it asks, it never assumes, and
// declining leaves a working tool behind — `daybook scan` by hand is a complete
// workflow.
func offerService(in *bufio.Reader, cfg config.Config, _ bool) {
	// Deliberately re-checked here rather than trusting the caller's captured
	// value. `interactive` is sampled once at the top of Run, but /dev/null is
	// a character device, so it starts out true and only becomes false when the
	// first read hits EOF. Trusting the stale bool made a non-interactive
	// `daybook init --force < /dev/null` install a real LaunchAgent that
	// nobody asked for — the one action in this wizard with a side effect
	// outside its own config directory.
	interactive := stdinIsInteractive()
	for _, c := range svc.Conflicts() {
		warn("a system-level registration exists and cannot work: %s", c)
		note("it runs as root, so it cannot read your keychain, your git identity,")
		note("or your transcripts — remove it, then install the user one below")
	}

	if installed, running, _ := svc.Status(cfg); installed {
		if running {
			ok("%s already installed and running", svc.AutoStartKind())
		} else {
			note("installed but stopped — starting it")
			if err := svc.Control(cfg, "start"); err != nil {
				warn("could not start: %v", err)
				note("start it:  daybook service start")
				return
			}
			ok("started")
		}
		return
	}

	note("installs a %s that runs as YOU — so it can read the `claude` login", svc.AutoStartKind())
	note("and your git identity. Runs at %s.", cfg.Schedule.At)
	if !interactive {
		note("install it later:  daybook service install")
		return
	}
	if strings.ToLower(ask(in, "      install it now? [Y/n]: ", "y")) == "n" {
		note("later:  daybook service install")
		return
	}
	if err := svc.Control(cfg, "install"); err != nil {
		warn("install failed: %v", err)
		note("retry:  daybook service install")
		return
	}
	if err := svc.Control(cfg, "start"); err != nil {
		ok("installed — it starts at your next login")
		note("start now:  daybook service start")
	} else {
		ok("installed and started")
	}
	if n := svc.PostInstallNote(); n != "" {
		note("%s", n)
	}
}

// defaultRepoRoot guesses where this person keeps code, preferring a directory
// that actually contains repositories over one that merely exists.
func defaultRepoRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~"
	}
	for _, cand := range []string{"code", "src", "Projects", "projects", "dev", "Desktop", "Documents"} {
		p := filepath.Join(home, cand)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			if hasRepo(p, 3) {
				return "~/" + cand
			}
		}
	}
	return "~"
}

func hasRepo(root string, depth int) bool {
	found := false
	base := strings.Count(filepath.Clean(root), string(os.PathSeparator))
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if strings.Count(filepath.Clean(p), string(os.PathSeparator))-base > depth {
			return filepath.SkipDir
		}
		if d.Name() == ".git" {
			found = true
		}
		return nil
	})
	return found
}

// eof latches once a read fails, so a non-interactive run stops printing
// prompts nobody will answer.
//
// The ModeCharDevice test alone is not enough: /dev/null IS a character
// device, so `daybook init < /dev/null` passes it and then EOFs on the first
// read. Only an actual read tells you whether anyone is there.
var eof bool

func stdinIsInteractive() bool {
	if eof {
		return false
	}
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

func ask(in *bufio.Reader, prompt, def string) string {
	if !stdinIsInteractive() {
		return def
	}
	fmt.Print(prompt)
	line, err := in.ReadString('\n')
	if err != nil {
		eof = true
		fmt.Println()
		return def
	}
	if s := strings.TrimSpace(line); s != "" {
		return s
	}
	return def
}

func tryRun(bin string, args ...string) string {
	out, err := exec.Command(bin, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
