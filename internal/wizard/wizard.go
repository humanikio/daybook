// Package wizard is the guided first-run setup.
//
// DESIGN RULES, each one learned the hard way:
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
	"sort"
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

const total = 6

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
	step(2, total, "Where does your code live?")

	fmt.Println()
	note("daybook scans these folders for git repositories. Point it at")
	note("wherever you keep projects — you can add more than one.")
	fmt.Println()
	note("Enter a path and press return. Leave it blank to finish.")
	note("You can change this any time:  %sdaybook watch <path>%s", bold, reset)
	fmt.Println()

	def := defaultRepoRoot()

	for i := 1; ; i++ {
		var prompt, fallback string
		if i == 1 {
			prompt = fmt.Sprintf("  %s%d%s   path [%s]: ", bold, i, reset, def)
			fallback = def
		} else {
			prompt = fmt.Sprintf("  %s%d%s   path (blank to finish): ", bold, i, reset)
			fallback = ""
		}

		entry := ask(in, prompt, fallback)
		if strings.TrimSpace(entry) == "" {
			if len(cfg.Watch.Repos) == 0 && i == 1 {
				// Refusing to proceed with nothing would be worse than a root
				// that finds nothing: the config is editable and `daybook watch`
				// exists. But say what happened.
				warn("no folders added — `daybook watch <path>` before your first scan")
			}
			break
		}

		abs := config.Expand(strings.TrimSpace(entry))
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			warn("%s is not a directory — skipped", entry)
			i--
			continue
		}
		if already(cfg, abs) {
			warn("already added")
			i--
			continue
		}

		before := len(vcs.Discover(cfg))
		cfg.Watch.Repos = append(cfg.Watch.Repos, config.RepoRoot{Path: strings.TrimSpace(entry), Depth: 4})
		found := len(vcs.Discover(cfg)) - before
		if found == 0 {
			warn("no git repositories under there")
			note("add it anyway, or try a parent folder")
		} else {
			ok("%d repositories", found)
		}
		if !stdinIsInteractive() {
			break
		}
	}

	allRepos := vcs.Discover(cfg)
	fmt.Println()
	if len(cfg.Watch.Repos) > 0 {
		ok("watching %s, %d repositories", plural(len(cfg.Watch.Repos), "folder"), len(allRepos))
	}

	noRemote := 0
	for _, r := range allRepos {
		if !vcs.Status(r.Root, false).HasRemote {
			noRemote++
		}
	}
	if noRemote > 0 {
		warn("%s no git remote", plural2(noRemote, "repository has", "repositories have"))
		note("\"shipped\" means off this machine, so those have no bar to clear;")
		note("output.no_remote=%q counts a commit as done for them", cfg.Output.NoRemote)
	}

	fmt.Println()
	authors := config.DetectAuthors()
	defAuthor := strings.Join(authors, ",")
	email := ask(in, fmt.Sprintf("      your commit email [%s]: ", defAuthor), defAuthor)
	var list []string
	for _, e := range strings.Split(email, ",") {
		if e = strings.TrimSpace(e); e != "" {
			list = append(list, e)
		}
	}
	cfg.Identity.Authors = list
	switch {
	case len(list) == 0:
		warn("no author set — every commit in these repos will count as yours")
	case len(list) == 1:
		ok("counting commits by %s", list[0])
	default:
		ok("counting commits by %s", strings.Join(list, " and "))
		note("this directory has its own git identity — both are counted")
	}

	// Check the answer against reality before moving on.
	//
	// An address that authors nothing produces a report with hours, streams and
	// prompts and zero commits — which reads as a quiet day, not a typo. That
	// happened on the first real setup of this tool, twice, because the address
	// someone THINKS they commit under and the one git records are different
	// things. Asking git here costs one call and turns it into a question.
	if len(list) > 0 && len(allRepos) > 0 {
		if real := authorsSeen(allRepos); len(real) > 0 && !anyMatch(list, real) {
			fmt.Println()
			warn("none of those have committed in these repos recently")
			note("git records these instead:")
			for _, a := range real {
				note("  %s", a)
			}
			if strings.ToLower(ask(in, "      use those instead? [Y/n]: ", "y")) != "n" {
				cfg.Identity.Authors = emailsOf(real)
				ok("counting commits by %s", strings.Join(cfg.Identity.Authors, " and "))
			} else {
				note("keeping it — `daybook scan` will tell you if nothing matches")
			}
		}
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
	step(4, total, "Where should reports go?")

	fmt.Println()
	note("One markdown file per day. Put it somewhere you will actually open —")
	note("a report you never read is not a report. It holds your prompt text,")
	note("so it is also a folder that shows up in a screen share.")
	fmt.Println()

	// Ask for the PARENT and create a named folder inside it. Asking for the
	// final path instead invites someone to type ~/Documents, at which point
	// outputs/, raw/ and state/ land loose in their Documents folder — three
	// unexplained directories they did not ask for and will not connect to this.
	parent, name := splitOutput(cfg.Output.Root)
	parent = ask(in, fmt.Sprintf("      create it in [%s]: ", parent), parent)
	name = ask(in, fmt.Sprintf("      folder name [%s]: ", name), name)

	parent = strings.TrimSpace(parent)
	name = strings.TrimSpace(name)
	if name == "" {
		name = "daybook"
	}
	cfg.Output.Root = filepath.Join(parent, name)

	abs := config.Expand(cfg.Output.Root)
	existed := false
	if fi, err := os.Stat(abs); err == nil && fi.IsDir() {
		existed = true
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return fmt.Errorf("could not create %s: %w", cfg.Output.Root, err)
	}
	if existed {
		ok("%s  (already there)", cfg.Output.Root)
	} else {
		ok("%s  (created)", cfg.Output.Root)
	}
	note("change it later:  %sdaybook config set output.root <path>%s", bold, reset)

	// ---------------------------------------------------------------- 5
	step(5, total, "Writing config")

	body := config.Render(cfg)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return err
	}
	for _, d := range []string{cfg.OutputsDir(), cfg.RawDir(), cfg.StateDir()} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	ok("config  → %s", path)
	ok("reports → %s", cfg.OutputsDir())

	// ---------------------------------------------------------------- 6
	step(6, total, "Schedule it")
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
		if err != nil {
			return nil
		}
		if found {
			// SkipAll, not nil. Returning nil here kept walking the entire tree
			// after the answer was already known — 28 seconds against 60ms for
			// the equivalent find, because "keep going" also means stat every
			// file in every node_modules it had already passed.
			return filepath.SkipAll
		}
		if !d.IsDir() {
			return nil
		}
		n := d.Name()
		// Prune the directories that hold almost all the files and none of the
		// repositories. Without this the walk pays for every dependency tree
		// under the root.
		if n == "node_modules" || n == "vendor" || n == "Pods" || n == "Library" ||
			(len(n) > 1 && n[0] == '.' && n != ".git") {
			return filepath.SkipDir
		}
		if strings.Count(filepath.Clean(p), string(os.PathSeparator))-base > depth {
			return filepath.SkipDir
		}
		if n == ".git" {
			found = true
			return filepath.SkipAll
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

// splitOutput turns a stored output root back into the parent and folder name
// the wizard asks for, so re-running it offers what you chose last time rather
// than the built-in default.
func splitOutput(root string) (parent, name string) {
	root = strings.TrimRight(strings.TrimSpace(root), "/")
	if root == "" {
		return "~/Desktop", "daybook"
	}
	parent, name = filepath.Split(root)
	parent = strings.TrimRight(parent, "/")
	if parent == "" {
		parent = "~/Desktop"
	}
	if name == "" {
		name = "daybook"
	}
	return parent, name
}

// authorsSeen lists who has actually committed in these repos lately.
//
// 30 days rather than the scan window: setup often happens on a quiet day, and
// "nobody committed in the last 24 hours" is not evidence that an address is
// wrong.
func authorsSeen(repos []vcs.Repo) []string {
	seen := map[string]bool{}
	var out []string
	since := time.Now().AddDate(0, 0, -30)
	for _, r := range repos {
		for _, a := range vcs.Authors(r.Root, since, time.Now()) {
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	sort.Strings(out)
	return out
}

func anyMatch(want []string, seen []string) bool {
	for _, w := range want {
		lw := strings.ToLower(strings.TrimSpace(w))
		for _, s := range seen {
			if lw != "" && strings.Contains(strings.ToLower(s), lw) {
				return true
			}
		}
	}
	return false
}

// emailsOf turns "Name <addr>" into addr, so the stored filter is the thing
// git actually matches on.
func emailsOf(authors []string) []string {
	var out []string
	for _, a := range authors {
		if i := strings.Index(a, "<"); i >= 0 {
			if j := strings.Index(a[i:], ">"); j > 0 {
				out = append(out, a[i+1:i+j])
				continue
			}
		}
		out = append(out, a)
	}
	return out
}

// already reports whether a root is configured, comparing resolved paths so
// "~/code" and the absolute form are the same entry.
func already(cfg config.Config, abs string) bool {
	for _, r := range cfg.Watch.Repos {
		if config.Expand(r.Path) == abs {
			return true
		}
	}
	return false
}

func plural2(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

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
