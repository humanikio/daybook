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

	"github.com/tndigitalmark/claude-code-daybook/internal/config"
	"github.com/tndigitalmark/claude-code-daybook/internal/vcs"
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

const total = 4

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
	if interactive {
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
	if interactive {
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
	if interactive {
		at = ask(in, fmt.Sprintf("      time [%s]: ", cfg.Schedule.At), cfg.Schedule.At)
		if _, err := time.Parse("15:04", at); err != nil {
			warn("%q is not HH:MM — keeping %s", at, cfg.Schedule.At)
			at = cfg.Schedule.At
		}
	}
	cfg.Schedule.At = at
	ok("daily at %s", at)
	note("scheduling itself lands in v3 (daybook service install);")
	note("until then run `daybook scan` whenever you like — it is idempotent")

	// ---------------------------------------------------------------- 4
	step(4, total, "Writing config")

	body := renderConfig(cfg)
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

	fmt.Printf("\n  %snext%s\n", bold, reset)
	fmt.Printf("    daybook scan     run one now against the last %s\n", cfg.Window.Length)
	fmt.Printf("    daybook day      read it\n")
	fmt.Printf("    daybook verify   check everything is wired up\n\n")
	return nil
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

// renderConfig writes the config with its comments intact.
//
// Generated rather than templated from a struct so the file a person opens
// explains itself — the reason a value is what it is matters more than the
// value, and a marshalled struct would throw all of that away.
func renderConfig(cfg config.Config) []byte {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+"\n", a...) }

	w("# daybook config. Precedence: env vars > this file > built-in defaults.")
	w("#")
	w("# Every string is quoted on purpose. Under YAML 1.1 an unquoted 12:00 is")
	w("# the integer 43200, NO is false, and 010 is 8.")
	w("")
	w("watch:")
	w("  agents:")
	for _, a := range cfg.Watch.Agents {
		w("    - { source: %q, path: %q }", a.Source, a.Path)
	}
	w("  repos:")
	for _, r := range cfg.Watch.Repos {
		w("    - { path: %q, depth: %d }", r.Path, r.Depth)
	}
	w("  # `git push` updates the local tracking ref, so the unpushed count is")
	w("  # already right for anything sent from this machine. Turn this on only")
	w("  # to notice pushes made somewhere else; it costs a round trip per repo.")
	w("  fetch: false")
	w("  ignore: []")
	w("")
	w("window:")
	w("  length: %q", cfg.Window.Length)
	w("  # window  — report only messages inside the window (a week-old session")
	w("  #           contributes today's work, not its whole history)")
	w("  # session — report the entire session, every day it is active")
	w("  scope: %q", cfg.Window.Scope)
	w("  stale_after: %q", cfg.Window.StaleAfter)
	w("")
	w("schedule:")
	w("  at: %q", cfg.Schedule.At)
	w("  days: []            # empty = every day")
	w("  catch_up: %v         # asleep at `at`? run on wake rather than skip the day", cfg.Schedule.CatchUp)
	w("")
	w("identity:")
	if len(cfg.Identity.Authors) > 0 {
		w("  authors: [%q]", cfg.Identity.Authors[0])
	} else {
		w("  authors: []        # empty = detect from git config user.email")
	}
	w("  machine: %q          # empty = hostname; namespaces output files", cfg.Identity.Machine)
	w("")
	w("output:")
	w("  root: %q", cfg.Output.Root)
	w("  # The bar for repos with no remote, where \"shipped\" is undefined.")
	w("  no_remote: %s      # committed | exclude", cfg.Output.NoRemote)
	w("")
	w("narrate:")
	w("  enabled: false      # v2. uses the claude you are already signed in with")
	w("  binary: \"\"")
	w("  timeout: %q", cfg.Narrate.Timeout)
	w("")
	w("privacy:")
	w("  # Redaction runs before anything reaches disk. Prompts carry pasted")
	w("  # secrets far more often than people expect.")
	w("  keep_raw_prompts: %v", cfg.Privacy.KeepRawPrompts)
	w("  redact:")
	for _, r := range cfg.Privacy.Redact {
		w("    - { name: %q, pattern: %q }", r.Name, r.Pattern)
	}
	w("")
	w("# Group repos into businesses for the shipped-to table. Optional.")
	w("# business:")
	w("#   - { name: \"Acme\", repos: [\"acme-*\", \"acme\"] }")
	return []byte(b.String())
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
