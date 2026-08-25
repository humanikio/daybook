package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/humanikio/daybook/internal/config"
	"github.com/humanikio/daybook/internal/schedule"
	"github.com/humanikio/daybook/internal/vcs"
)

// Config mutation from the CLI.
//
// Hand-editing YAML is fine for someone who already knows the shape of the
// file, and a wall of it is the wrong answer for "add this directory". These
// commands cover the two things people actually change after setup — where it
// looks, and when it runs — and leave everything else to the file.
//
// Every one of them re-reads, mutates, and writes the whole file through
// config.Render, so a value set here looks identical to one the wizard wrote.

func cmdWatch(args []string) error {
	args = flagsFirst(args)
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	depth := fs.Int("depth", 4, "how deep to search for repositories")
	agent := fs.Bool("agent", false, "add a transcript source rather than a repo root")
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		// No path: show what is being watched. A bare `daybook watch` asking
		// "watch what?" would be right and useless; showing the answer is
		// what someone typing it actually wanted.
		fmt.Println("transcript sources:")
		for _, a := range cfg.Watch.Agents {
			p := config.Expand(a.Path)
			n, _ := filepath.Glob(filepath.Join(p, "*", "*.jsonl"))
			fmt.Printf("  %-40s %s  (%d transcripts)\n", a.Path, a.Source, len(n))
		}
		fmt.Println("repo roots:")
		for _, r := range cfg.Watch.Repos {
			fmt.Printf("  %-40s depth %d\n", r.Path, r.Depth)
		}
		fmt.Printf("  → %d repositories discovered\n", len(vcs.Discover(cfg)))
		return nil
	}

	raw := rest[0]
	abs := config.Expand(raw)
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a directory I can read", raw)
	}
	// Store the path as typed. A ~ that stays a ~ survives being copied to
	// another machine, and these config files get copied.
	stored := tildify(abs, raw)

	if *agent {
		for _, a := range cfg.Watch.Agents {
			if config.Expand(a.Path) == abs {
				return fmt.Errorf("already watching %s", raw)
			}
		}
		cfg.Watch.Agents = append(cfg.Watch.Agents, config.AgentSource{Source: "claude-code", Path: stored})
		if err := config.Save(cfg); err != nil {
			return err
		}
		n, _ := filepath.Glob(filepath.Join(abs, "*", "*.jsonl"))
		fmt.Printf("watching %s — %d transcripts\n", stored, len(n))
		return nil
	}

	for _, r := range cfg.Watch.Repos {
		if config.Expand(r.Path) == abs {
			return fmt.Errorf("already watching %s", raw)
		}
	}
	before := len(vcs.Discover(cfg))
	cfg.Watch.Repos = append(cfg.Watch.Repos, config.RepoRoot{Path: stored, Depth: *depth})
	after := vcs.Discover(cfg)
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("watching %s (depth %d) — %d new repositories, %d total\n",
		stored, *depth, len(after)-before, len(after))
	if len(after) == before {
		// Silence here would read as success. The usual cause is depth.
		fmt.Printf("  nothing found — try a larger --depth, or check the path holds git repos\n")
	}
	return nil
}

func cmdUnwatch(args []string) error {
	args = flagsFirst(args)
	fs := flag.NewFlagSet("unwatch", flag.ContinueOnError)
	agent := fs.Bool("agent", false, "remove a transcript source rather than a repo root")
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("which path? see `daybook watch`")
	}
	abs := config.Expand(rest[0])

	if *agent {
		var kept []config.AgentSource
		for _, a := range cfg.Watch.Agents {
			if config.Expand(a.Path) != abs {
				kept = append(kept, a)
			}
		}
		if len(kept) == len(cfg.Watch.Agents) {
			return fmt.Errorf("not watching %s", rest[0])
		}
		cfg.Watch.Agents = kept
	} else {
		var kept []config.RepoRoot
		for _, r := range cfg.Watch.Repos {
			if config.Expand(r.Path) != abs {
				kept = append(kept, r)
			}
		}
		if len(kept) == len(cfg.Watch.Repos) {
			return fmt.Errorf("not watching %s", rest[0])
		}
		cfg.Watch.Repos = kept
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("no longer watching %s\n", rest[0])
	return nil
}

func cmdSchedule(args []string) error {
	// Go's flag package stops parsing at the first non-flag argument, so
	// `schedule 22:15 --days mon,fri` silently dropped --days into positionals
	// and reported success having changed only the time. Hoist flags ahead of
	// positionals rather than making people remember the order.
	args = flagsFirst(args)
	fs := flag.NewFlagSet("schedule", flag.ContinueOnError)
	days := fs.String("days", "", "comma-separated weekdays, or \"every\" for daily")
	catchUp := fs.String("catch-up", "", "true|false — run a missed slot on wake")
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	changed := false

	if rest := fs.Args(); len(rest) > 0 {
		if _, err := time.Parse("15:04", rest[0]); err != nil {
			return fmt.Errorf("want a 24-hour time like 22:00, got %q", rest[0])
		}
		cfg.Schedule.At = rest[0]
		changed = true
	}
	if *days != "" {
		if strings.EqualFold(*days, "every") || strings.EqualFold(*days, "all") {
			cfg.Schedule.Days = nil
		} else {
			cfg.Schedule.Days = strings.Split(strings.ToLower(strings.ReplaceAll(*days, " ", "")), ",")
		}
		changed = true
	}
	if *catchUp != "" {
		b, err := strconv.ParseBool(*catchUp)
		if err != nil {
			return fmt.Errorf("--catch-up wants true or false, got %q", *catchUp)
		}
		cfg.Schedule.CatchUp = b
		changed = true
	}

	if changed {
		if err := cfg.Validate(); err != nil {
			return err
		}
		if err := config.Save(cfg); err != nil {
			return err
		}
	}

	when := "every day"
	if len(cfg.Schedule.Days) > 0 {
		when = strings.Join(cfg.Schedule.Days, ", ")
	}
	fmt.Printf("runs at %s, %s", cfg.Schedule.At, when)
	if cfg.Schedule.CatchUp {
		fmt.Print(" (catches up after sleep)")
	} else {
		fmt.Print(" (a missed run is skipped)")
	}
	fmt.Println()
	if next, ok := schedule.Next(cfg, time.Now()); ok {
		fmt.Printf("next: %s\n", next.Format("Mon 2 Jan 15:04"))
	}
	if changed {
		if installed, running, _ := svcStatus(cfg); installed && running {
			// The service re-reads config every tick, so there is nothing to
			// restart — worth saying, because with most daemons there would be.
			fmt.Println("the running scheduler picks this up on its next tick — no restart needed")
		}
	}
	return nil
}

// settable is the allow-list for `config set`.
//
// Deliberately explicit rather than reflection over yaml tags. Reflection would
// accept any key that happens to exist, including ones that need validation or
// are lists, and would silently drift as the struct changes. A short list that
// says no to everything else is easier to trust.
var settable = map[string]func(*config.Config, string) error{
	"narrate.enabled":          func(c *config.Config, v string) error { return setBool(&c.Narrate.Enabled, v) },
	"narrate.provider":         func(c *config.Config, v string) error { c.Narrate.Provider = v; return nil },
	"narrate.model":            func(c *config.Config, v string) error { c.Narrate.Model = v; return nil },
	"narrate.effort":           func(c *config.Config, v string) error { c.Narrate.Effort = v; return nil },
	"narrate.timeout":          func(c *config.Config, v string) error { c.Narrate.Timeout = v; return nil },
	"window.length":            func(c *config.Config, v string) error { c.Window.Length = v; return nil },
	"window.scope":             func(c *config.Config, v string) error { c.Window.Scope = v; return nil },
	"window.stale_after":       func(c *config.Config, v string) error { c.Window.StaleAfter = v; return nil },
	"watch.fetch":              func(c *config.Config, v string) error { return setBool(&c.Watch.Fetch, v) },
	"output.root":              func(c *config.Config, v string) error { c.Output.Root = v; return nil },
	"output.no_remote":         func(c *config.Config, v string) error { c.Output.NoRemote = v; return nil },
	"identity.machine":         func(c *config.Config, v string) error { c.Identity.Machine = v; return nil },
	"identity.authors":         func(c *config.Config, v string) error { c.Identity.Authors = splitCSV(v); return nil },
	"privacy.keep_raw_prompts": func(c *config.Config, v string) error { return setBool(&c.Privacy.KeepRawPrompts, v) },
}

func cmdConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	cfg, _, err := loadCfg(fs, args)
	if err != nil {
		return err
	}
	rest := fs.Args()

	if len(rest) == 0 {
		fmt.Println(cfg.Path())
		fmt.Println()
		fmt.Print(string(config.Render(cfg)))
		return nil
	}
	if rest[0] != "set" {
		return fmt.Errorf("usage: daybook config [set <key> <value>]")
	}
	if len(rest) < 3 {
		fmt.Fprintln(os.Stderr, "usage: daybook config set <key> <value>\n\nkeys:")
		var keys []string
		for k := range settable {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for _, k := range keys {
			fmt.Fprintf(os.Stderr, "  %s\n", k)
		}
		return fmt.Errorf("missing key or value")
	}

	set, ok := settable[rest[1]]
	if !ok {
		return fmt.Errorf("%q is not a settable key — run `daybook config set` for the list, "+
			"or edit %s directly", rest[1], cfg.Path())
	}
	if err := set(&cfg, strings.Join(rest[2:], " ")); err != nil {
		return err
	}
	// Validate BEFORE writing. A config file that will not load is a worse
	// outcome than a rejected command, and this is the only gate between the
	// two.
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("%s = %s\n", rest[1], strings.Join(rest[2:], " "))
	return nil
}

// flagsFirst reorders argv so every flag precedes every positional.
//
// Only needed because the standard flag package treats the first bare word as
// the end of the flags. A flag that is accepted, ignored, and reported as
// success is worse than one that errors.
func flagsFirst(args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		// `--days mon,fri` — the value is a separate argument unless it was
		// written as `--days=mon,fri`.
		if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			if _, isBool := boolFlags[strings.TrimLeft(a, "-")]; !isBool {
				i++
				flags = append(flags, args[i])
			}
		}
	}
	return append(flags, pos...)
}

// boolFlags take no value, so the word after them is a positional.
var boolFlags = map[string]struct{}{
	"agent": {}, "stdout": {}, "narrate": {}, "force": {},
}

func setBool(dst *bool, v string) error {
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fmt.Errorf("want true or false, got %q", v)
	}
	*dst = b
	return nil
}

func splitCSV(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// tildify keeps a path portable when the user typed it that way.
func tildify(abs, raw string) string {
	if strings.HasPrefix(raw, "~") {
		return raw
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(abs, home+string(filepath.Separator)) {
		return "~" + strings.TrimPrefix(abs, home)
	}
	return abs
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
